#!/bin/sh
# Stable release bootstrap; no arguments install a system service as root.
set -eu
command -v python3 >/dev/null 2>&1 || { echo '需要 Python 3.10+ / Python 3.10+ is required' >&2; exit 1; }
exec python3 - "$@" <<'PY'
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

REPO = 'aiprofesserzhaofeng8-jpg/codearts2api-panel'
API = 'https://api.github.com/repos/' + REPO + '/releases/'
BASE = 'https://github.com/' + REPO + '/releases/download/'

# The shell feeds Python through stdin; prompts must use the controlling terminal.
# Interaction follows the curl-pipe pattern used by Sub2API's deploy/install.sh.
def system_language():
    # Match POSIX message-locale precedence without requiring the locale to be installed.
    value = next((os.environ[key] for key in ('LC_ALL', 'LC_MESSAGES', 'LANG') if os.environ.get(key)), '')
    code = value.lower().split('.')[0].split('@')[0].replace('-', '_')
    if code.startswith('en_') or code in ('en', 'c', 'posix'):
        return 'en'
    return 'zh'


def initial_language(argv):
    language = system_language()
    for index, value in enumerate(argv):
        if value == '--':
            break
        candidate = value.split('=', 1)[1] if value.startswith('--lang=') else argv[index + 1] if value == '--lang' and index + 1 < len(argv) else None
        if candidate in ('zh', 'en', 'auto'):
            language = system_language() if candidate == 'auto' else candidate
    return language


LANGUAGE = system_language()


def message(zh, en):
    return en if LANGUAGE == 'en' else zh


def colored(text, stream, code):
    if stream.isatty() and os.environ.get('TERM') != 'dumb' and 'NO_COLOR' not in os.environ:
        return f'\033[{code}m{text}\033[0m'
    return text


def emit(text, level='info', file=None):
    stream = sys.stdout if file is None else file
    labels = {'info': message('信息', 'INFO'), 'success': message('成功', 'SUCCESS'),
              'warning': message('提示', 'WARNING'), 'error': message('错误', 'ERROR')}
    codes = {'info': '34', 'success': '32', 'warning': '33', 'error': '31', 'title': '36'}
    prefix = f'[{labels[level]}] ' if level in labels else ''
    print(colored(prefix + text, stream, codes[level]), file=stream, flush=True)


class InstallerParser(argparse.ArgumentParser):
    def format_help(self):
        text = super().format_help()
        if LANGUAGE == 'zh':
            text = text.replace('usage: ', '用法：').replace('positional arguments:', '操作：').replace('options:', '选项：')
        return text

    def error(self, detail):
        if LANGUAGE == 'zh':
            patterns = (
                (r'unrecognized arguments: (.*)', r'无法识别的参数：\1'),
                (r'argument (.*): invalid choice: (.*) \(choose from (.*)\)', r'参数 \1 的值 \2 无效；可选值：\3'),
                (r"argument (.*): invalid int value: (.*)", r'参数 \1 必须是整数，收到：\2'),
                (r'argument (.*): expected one argument', r'参数 \1 缺少值'),
                (r'argument (.*): not allowed with argument (.*)', r'参数 \1 不能与 \2 同时使用'),
            )
            translated = next((re.sub(pattern, replacement, detail) for pattern, replacement in patterns if re.fullmatch(pattern, detail)), None)
            detail = translated or '参数无效，请使用 --help 查看用法。'
        emit(detail, 'error', file=sys.stderr)
        self.exit(2)


class InstallCancelled(Exception):
    pass


def prompt(reader, writer, label, default):
    writer.write(colored(f'{label} [{default}]: ', writer, '34'))
    writer.flush()
    line = reader.readline()
    if not line:
        raise InstallCancelled()
    return line.strip() or default


def open_terminal():
    # Testing existence/access alone is insufficient: CI may have /dev/tty but
    # no controlling terminal. Never read from the curl/script input stream.
    from contextlib import ExitStack
    stack = ExitStack()
    try:
        reader = stack.enter_context(open('/dev/tty', 'r', encoding='utf-8'))
        writer = stack.enter_context(open('/dev/tty', 'w', encoding='utf-8', buffering=1))
        if not reader.isatty() or not writer.isatty():
            raise OSError('not a terminal')
        return stack, reader, writer
    except OSError:
        stack.close()
        return None


def configure(args):
    global LANGUAGE
    LANGUAGE = args.lang if args.lang in ('zh', 'en') else system_language()
    terminal = None
    if not args.non_interactive and args.command == 'install' and (args.system or args.interactive):
        terminal = open_terminal()
    require(not args.interactive or terminal is not None,
            message('交互安装需要可用终端；自动安装请使用 --non-interactive。',
                    'Interactive installation requires a terminal; use --non-interactive for automation.'))
    if terminal is not None:
        stack, reader, writer = terminal
        with stack:
            if args.lang in (None, 'auto'):
                emit('\n请选择语言 / Select language', 'title', file=writer)
                writer.write('  1) 中文\n  2) English\n')
                default = '2' if LANGUAGE == 'en' else '1'
                writer.write(message('回车跟随系统语言：中文\n', 'Press Enter to follow the system language: English\n'))
                while True:
                    language = prompt(reader, writer, message('请选择', 'Choice'), default).lower()
                    if language in ('1', 'zh', '2', 'en'):
                        LANGUAGE = 'en' if language in ('2', 'en') else 'zh'
                        break
                    emit(message('请输入 1 或 2。', 'Enter 1 or 2.'), 'warning', file=writer)
            emit('\n' + message('CodeArts2API 系统服务安装向导' if args.system else 'CodeArts2API 安装向导',
                                         'CodeArts2API system service installer' if args.system else 'CodeArts2API installer'), 'title', file=writer)
            writer.write(message('0.0.0.0 监听所有网卡；127.0.0.1 仅本机访问。\n',
                                 '0.0.0.0 listens on all interfaces; 127.0.0.1 is local only.\n'))
            if args.host is None and args.system:
                while True:
                    host = prompt(reader, writer, message('服务器监听地址', 'Listen address'), '127.0.0.1')
                    if host in ('127.0.0.1', '0.0.0.0'):
                        args.host = host
                        break
                    writer.write(message('请输入 127.0.0.1 或 0.0.0.0。\n', 'Enter 127.0.0.1 or 0.0.0.0.\n'))
            if args.port is None:
                while True:
                    value = prompt(reader, writer, message('服务器端口 (建议 1024-65535)', 'Server port (recommended: 1024-65535)'), '10087')
                    if re.fullmatch(r'[0-9]{1,5}', value) and 1 <= int(value) <= 65535:
                        args.port = int(value)
                        break
                    writer.write(message('端口必须是 1-65535 之间的整数。\n', 'Port must be an integer from 1 to 65535.\n'))
            args.host = args.host or '127.0.0.1'
            if args.host == '0.0.0.0':
                emit(message('请限制未初始化面板的外部访问；任何可访问者都能创建首个管理员。\n',
                                     'Restrict access until setup is complete; anyone who can reach setup can create the first administrator.'), 'warning', file=writer)
            writer.write(message('安装目录：', 'Install directory: ') + str(args.root) + '\n')
            writer.write(message('监听地址：', 'Listen address: ') + f'{args.host}:{args.port}\n')
            while True:
                choice = prompt(reader, writer, message('继续安装？(Y/n)', 'Continue installation? (Y/n)'), 'Y').lower()
                if choice in ('y', 'yes'):
                    break
                if choice in ('n', 'no'):
                    raise InstallCancelled()
                writer.write(message('请输入 y 或 n。\n', 'Enter y or n.\n'))
    args.host = args.host or '127.0.0.1'
    args.port = args.port if args.port is not None else 10087


class InstallError(Exception):
    pass

def require(ok, message):
    if not ok:
        raise InstallError(message)

def version(value):
    require(re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+', value), message('仅支持稳定版本 vX.Y.Z', 'Only stable versions vX.Y.Z are supported'))
    return value

class Redirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        require(not req.full_url.startswith(API), message('版本 API 不允许重定向', 'Release API redirects are not allowed'))
        parsed = urllib.parse.urlsplit(newurl)
        require(parsed.scheme == 'https' and parsed.hostname in ('github.com', 'release-assets.githubusercontent.com', 'objects.githubusercontent.com') and parsed.port in (None, 443) and not parsed.username, message('下载重定向来源无效', 'Invalid download redirect origin'))
        return super().redirect_request(req, fp, code, msg, headers, newurl)

def download_label(url):
    parsed = urllib.parse.urlsplit(url)
    # Never expose signed redirect query strings or credentials in diagnostics.
    return f'{parsed.hostname or "GitHub"}/{Path(parsed.path).name}'


def http_failure(error):
    label = download_label(error.url)
    if error.code == 404:
        detail = message('附件或版本未找到，请核对发布内容。', 'Release or asset not found; check the published files.')
    elif error.code in (500, 502, 503, 504):
        detail = message('GitHub 下载服务暂时异常，请稍后重试。', 'The GitHub download service is temporarily unavailable; retry later.')
    elif error.code in (403, 429):
        detail = message('请求被拒绝或限流。', 'The request was denied or rate-limited.')
        if error.url.startswith(API):
            detail += message('可稍后重试或配置 GITHUB_TOKEN。', ' Retry later or configure GITHUB_TOKEN.')
    elif error.code == 618:
        detail = message('附件临时下载链接失效，请重新运行以获取新链接。', 'The temporary asset link expired; rerun to obtain a fresh link.')
    else:
        detail = message('下载请求失败，请检查网络或稍后重试。', 'Download request failed; check the network or retry later.')
    return f'HTTP {error.code} · {label} · {detail}'


def fetch(url, target, limit):
    require(not target.exists() and not target.is_symlink(),
            message('下载目标已存在，拒绝覆盖。', 'Download target exists; refusing to overwrite it.'))
    is_asset = url.startswith(BASE)
    attempts = 3 if is_asset else 1
    for attempt in range(attempts):
        # Retry from the original trusted asset URL, never an old signed redirect.
        # A unique query prevents an intermediate cache reusing an error/expired link.
        request_url = url
        if attempt:
            request_url += ('&' if '?' in url else '?') + 'codearts_retry=' + str(time.time_ns())
        headers = {'User-Agent': 'codearts2api-installer',
                   'Accept': 'application/vnd.github+json' if url.startswith(API) else 'application/octet-stream',
                   'Cache-Control': 'no-cache'}
        request = urllib.request.Request(request_url, headers=headers)
        if url.startswith(API) and os.environ.get('GITHUB_TOKEN'):
            request.add_header('Authorization', 'Bearer ' + os.environ['GITHUB_TOKEN'])
        created = False
        try:
            with urllib.request.build_opener(Redirects()).open(request, timeout=30) as source:
                with target.open('xb') as output:
                    created = True
                    size = 0
                    while chunk := source.read(1024 * 1024):
                        size += len(chunk)
                        require(size <= limit, message('下载超出大小限制', 'Download exceeds the size limit'))
                        output.write(chunk)
                    length = source.headers.get('Content-Length')
                    if length is not None and length.isdecimal() and size != int(length):
                        raise http.client.IncompleteRead(b'', int(length) - size)
            return
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead) as error:
            if created:
                target.unlink(missing_ok=True)
            retryable = not isinstance(error, urllib.error.HTTPError) or error.code in (408, 429, 500, 502, 503, 504, 618)
            if not retryable or attempt + 1 == attempts:
                raise
            if isinstance(error, urllib.error.HTTPError):
                reason = f'HTTP {error.code}'
                error.close()
            else:
                reason = message('连接中断或下载不完整', 'Connection interrupted or incomplete download')
            emit(message('下载暂时失败，重新获取链接后重试', 'Temporary download failure; refreshing the link and retrying') +
                 f' ({attempt + 2}/{attempts}) · {download_label(url)} · {reason}', 'warning')
            time.sleep(attempt + 1)
        except BaseException:
            if created:
                target.unlink(missing_ok=True)
            raise


class NoReleaseRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def public_latest_version():
    # Stop at GitHub's public latest -> tag redirect. Do not follow arbitrary
    # locations, scrape HTML, or send API credentials to this endpoint.
    url = 'https://github.com/' + REPO + '/releases/latest'
    request = urllib.request.Request(url, method='HEAD', headers={'User-Agent': 'codearts2api-installer'})
    try:
        with urllib.request.build_opener(NoReleaseRedirects()).open(request, timeout=20):
            raise InstallError(message('公开发布入口未返回稳定版本。', 'The public release entry did not return a stable version.'))
    except urllib.error.HTTPError as error:
        try:
            if error.code not in (301, 302, 303, 307, 308):
                raise
            location = urllib.parse.urljoin(url, error.headers.get('Location', ''))
            prefix = 'https://github.com/' + REPO + '/releases/tag/'
            require(location.startswith(prefix), message('公开发布跳转来源无效。', 'Invalid public release redirect origin.'))
            return version(location.removeprefix(prefix))
        finally:
            error.close()


def public_release(work, selected):
    tag = public_latest_version()
    # Public latest excludes drafts/prereleases. For an older explicitly pinned
    # tag, only the API can verify its stable-release flags; fail closed instead.
    require(selected == 'latest' or selected == tag,
            message('API 不可用时仅支持公开最新稳定版；指定旧版本请稍后重试或配置 GITHUB_TOKEN。',
                    'Without the API, only the public latest stable release is supported; retry later or configure GITHUB_TOKEN for older versions.'))
    path = work / 'public-SHA256SUMS'
    fetch(BASE + tag + '/SHA256SUMS', path, 65536)
    expected = checksums(path)
    # Derive download locations only from this repository and the validated tag;
    # checksum filenames cannot inject hosts, paths or shell commands.
    data = {'tag_name': tag, 'source': 'public-checksums', 'assets': [
        {'name': name, 'browser_download_url': BASE + tag + '/' + name}
        for name in [*expected, 'SHA256SUMS']
    ]}
    return tag, data


def release(work, selected):
    meta = work / 'release.json'
    url = API + ('latest' if selected == 'latest' else 'tags/' + version(selected))
    try:
        fetch(url, meta, 2 * 1024**2)
    except (urllib.error.URLError, TimeoutError) as error:
        # A missing release, bad credentials, malformed metadata, or a checksum
        # failure is not a reason to silently select a different version.
        if isinstance(error, urllib.error.HTTPError) and error.code not in (403, 429, 500, 502, 503, 504):
            raise
        if isinstance(error, urllib.error.HTTPError):
            error.close()
        emit(message('GitHub API 暂不可用，正在通过公开发布入口查询稳定版（无需 Token）…',
                     'GitHub API is unavailable; checking the public stable release entry without a token…'), 'warning')
        return public_release(work, selected)
    data = json.loads(meta.read_text())
    require(isinstance(data, dict) and not data.get('draft') and not data.get('prerelease'), message('没有可用的正式发布', 'No stable release is available'))
    tag = version(data.get('tag_name', ''))
    require(selected == 'latest' or tag == selected, message('返回版本不匹配', 'Release version mismatch'))
    return tag, data

def assets(data, tag, required):
    result = {}
    for item in data.get('assets', []):
        name = item.get('name')
        if name not in required:
            continue
        require(name not in result, message('发布附件重复', 'Duplicate release assets'))
        url = BASE + tag + '/' + name
        require(item.get('browser_download_url') == url, message('发布附件来源无效', 'Invalid release asset origin'))
        result[name] = url
    require(set(result) == set(required), message('该版本缺少安装附件，尚不能安装或升级', 'Required installation assets are missing from this release'))
    return result

def checksums(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([a-fA-F0-9]{64}) [ *]([A-Za-z0-9_.-]+)', line)
        require(match is not None and match[2] not in result, message('校验清单无效或重复', 'Invalid or duplicate checksum entries'))
        result[match[2]] = match[1].lower()
    return result

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            h.update(chunk)
    return h.hexdigest()

def main():
    global LANGUAGE
    LANGUAGE = initial_language(sys.argv[1:])
    require(sys.version_info >= (3, 10), message('需要 Python 3.10 或更新版本', 'Python 3.10 or newer is required'))
    p = InstallerParser(prog='install.sh', add_help=False, description=message('CodeArts2API 稳定版本安装入口；升级前请阅读发布说明。离线使用 panelctl.py。', 'CodeArts2API stable release installer. Read release notes before upgrading. Use panelctl.py offline.'))
    p.add_argument('-h', '--help', action='help', help=message('显示帮助并退出', 'Show help and exit'))
    p.add_argument('command', nargs='?', choices=['check', 'download', 'install', 'upgrade'])
    p.add_argument('--version', default='latest')
    p.add_argument('--root', type=Path)
    p.add_argument('--output', type=Path)
    p.add_argument('--name', default='managed')
    p.add_argument('--port', type=int, help=message('监听端口（默认 10087）', 'Listen port (default: 10087)'))
    p.add_argument('--system', action='store_true', help=message('安装系统级服务；无参数时默认启用', 'Install a system service (default without a command)'))
    p.add_argument('--host', choices=['127.0.0.1', '0.0.0.0'], help=message('监听地址（默认 127.0.0.1）', 'Listen address (default: 127.0.0.1)'))
    p.add_argument('--lang', choices=['auto', 'zh', 'en'], help=message('安装器语言（默认跟随系统）', 'Installer language (default: system)'))
    mode = p.add_mutually_exclusive_group()
    mode.add_argument('--interactive', action='store_true', help=message('强制显示安装向导', 'Require installation prompts'))
    mode.add_argument('--non-interactive', action='store_true', help=message('使用参数和默认值，不提问', 'Use arguments and defaults without prompting'))
    args = p.parse_args()
    LANGUAGE = args.lang if args.lang in ('zh', 'en') else system_language()
    args.system = args.system or args.command is None or (args.command in ('install', 'upgrade') and args.root is None and os.geteuid() == 0)
    args.command = args.command or 'install'
    require(not args.interactive or args.command == 'install', message('--interactive 仅用于 install', '--interactive is only supported for install'))
    if args.command == 'install' and args.port is not None:
        require(1 <= args.port <= 65535, message('端口必须是 1-65535 之间的整数', 'Port must be between 1 and 65535'))
    if args.system and args.command in ('install', 'upgrade'):
        require(os.geteuid() == 0, message('系统级安装/升级需要 root，请使用 curl … | sudo bash', 'System installation/upgrade requires root; use curl … | sudo bash'))
        args.root = args.root or Path('/opt/codearts2api')
        require(Path('/run/systemd/system').is_dir() and shutil.which('systemctl'), message('需要正在运行的 systemd 系统服务管理器', 'A running systemd system manager is required'))
        if args.command == 'install':
            require(not args.root.exists() and not args.root.is_symlink(), message('安装目录已存在；请使用 upgrade，未覆盖已有数据', 'Installation directory already exists; use upgrade. Existing data was not overwritten'))
    require(platform.system() == 'Linux', message('此安装入口仅支持 Linux', 'This installer only supports Linux'))
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(arch, message('此安装入口仅支持 amd64/arm64', 'This installer only supports amd64/arm64'))
    require(args.command not in ('install', 'upgrade') or args.root is not None, message('安装/升级需要 --root', 'Installation/upgrade requires --root'))
    require(args.command != 'download' or args.output is not None, message('下载需要 --output 指定尚不存在的目录', 'Download requires --output pointing to a new directory'))
    if args.command == 'install':
        require(not args.root.exists() and not args.root.is_symlink(), message('安装目录已存在；请使用 upgrade，未覆盖已有数据', 'Installation directory already exists; use upgrade. Existing data was not overwritten'))
        require(args.system or args.host in (None, '127.0.0.1'), message('自定义监听地址请使用 --system', 'A custom listen address requires --system'))
    configure(args)
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix='codearts-release-') as temp:
        work = Path(temp)
        emit(message('正在获取发布版本…', 'Fetching release information…'))
        tag, data = release(work, args.version)
        name = f'codearts2api-panel-{tag}-linux-{arch}.tar.gz'
        urls = assets(data, tag, [name, 'panelctl.py', 'SHA256SUMS'])
        emit(message('稳定版本：', 'Stable release: ') + tag)
        if args.command == 'check':
            if data.get('source') == 'public-checksums':
                emit(message('公开校验清单包含所需附件；安装时将下载并验证附件摘要和状态版本。',
                             'The public checksum list includes required assets; installation will download and verify them and check state compatibility.'))
            else:
                emit(message('发布附件齐全；安装时将继续校验摘要和状态版本。', 'Required assets are present; installation will verify checksums and state compatibility.'))
            return
        emit(message('正在下载校验清单…', 'Downloading checksums…'))
        if data.get('source') == 'public-checksums':
            (work / 'public-SHA256SUMS').replace(work / 'SHA256SUMS')
        else:
            fetch(urls['SHA256SUMS'], work / 'SHA256SUMS', 65536)
        expected = checksums(work / 'SHA256SUMS')
        for item in (name, 'panelctl.py'):
            require(item in expected, message('校验清单缺少必需附件', 'Required assets are missing from checksums'))
            emit(message('正在下载：', 'Downloading: ') + item)
            fetch(urls[item], work / item, 512 * 1024**2 if item == name else 2 * 1024**2)
            require(digest(work / item) == expected[item], message('附件摘要不匹配，未执行安装', 'Asset checksum mismatch; installation was not executed'))
        emit(message('附件校验通过。', 'Asset checksums verified.'), 'success')
        if args.command == 'download':
            # Exclusive directory creation avoids replacing previous downloads.
            args.output.mkdir(mode=0o700)
            for item in (name, 'panelctl.py', 'SHA256SUMS'):
                (args.output / item).write_bytes((work / item).read_bytes())
            emit(message('已校验并下载至：', 'Verified download saved to: ') + str(args.output))
            return
        command = [sys.executable, str(work / 'panelctl.py'), args.command, '--root', str(args.root), '--archive', str(work / name), '--sha256', expected[name]]
        if args.command == 'install':
            command += ['--name', args.name, '--port', str(args.port)]
            if args.system:
                capability = subprocess.run([sys.executable, str(work / 'panelctl.py'), 'install', '--help'], capture_output=True, text=True, check=True)
                require('--system' in capability.stdout, message('该正式版本尚不支持系统级安装，请等待包含新安装工具的版本发布', 'This release does not support system installation; choose a release with the new installer'))
                command += ['--system', '--host', args.host]
            else:
                require(args.host == '127.0.0.1', message('自定义监听地址请使用 --system', 'A custom listen address requires --system'))
        emit(message('正在安装服务…' if args.command == 'install' else '正在升级服务…',
                      'Installing service…' if args.command == 'install' else 'Upgrading service…'))
        # Released helpers predate language selection. Keep their successful
        # summary out of the UI and render one localized summary here. Preserve
        # original failure diagnostics, including external systemd errors.
        try:
            subprocess.run(command, check=True, capture_output=True, text=True)
        except subprocess.CalledProcessError as error:
            emit(message('维护工具执行失败，原始诊断如下：', 'Maintenance tool failed; original diagnostics follow:'), 'error', file=sys.stderr)
            for detail in (error.stdout, error.stderr):
                if detail:
                    print(detail.rstrip(), file=sys.stderr)
            raise InstallError(message('安装或升级未完成，请根据诊断检查服务状态。',
                                       'Installation or upgrade did not complete; check service status using the diagnostics.')) from None
        if args.command == 'install':
            emit(message('安装完成；打开面板设置管理员邮箱和密码。',
                         'Installation complete. Open the panel to set your administrator email and password.'), 'success')
            emit(message('访问地址：', 'Open: ') + f'http://127.0.0.1:{args.port}/' + message('（服务器本机）', ' (on the server)'))
            unit = 'codearts2api.service' if args.system and args.name == 'managed' else 'codearts2api-' + args.name + '.service'
            emit(message('服务状态：', 'Status: ') + f'{"sudo systemctl" if args.system else "systemctl --user"} status {unit}')
        else:
            emit(message('升级完成；保留旧程序和升级前备份。',
                         'Upgrade complete; the previous program and pre-upgrade backup are retained.'), 'success')

if __name__ == '__main__':
    try:
        main()
    except (InstallCancelled, KeyboardInterrupt):
        emit(message('操作已取消。', 'Operation cancelled.'), 'warning', file=sys.stderr)
        sys.exit(130)
    except urllib.error.HTTPError as error:
        emit(http_failure(error), 'error', file=sys.stderr)
        error.close()
        sys.exit(1)
    except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead):
        emit(message('GitHub 连接失败或超时；公开发布页或附件下载也不可达，请检查网络后重试。',
                     'GitHub connection failed or timed out; the public release page or assets could not be reached. Check the network and retry.'), 'error', file=sys.stderr)
        sys.exit(1)
    except Exception as error:
        emit(str(error) if isinstance(error, InstallError) else message('获取或安装失败；请检查网络、发布附件和部署状态。', 'Download or installation failed; check network, release assets and deployment state.'), 'error', file=sys.stderr)
        sys.exit(1)
PY
