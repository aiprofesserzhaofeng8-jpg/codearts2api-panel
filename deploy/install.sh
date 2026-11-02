#!/bin/sh
# Stable release bootstrap; no arguments install a system service as root.
set -eu
command -v python3 >/dev/null 2>&1 || { echo '需要 Python 3.10 或更新版本' >&2; exit 1; }
exec python3 - "$@" <<'PY'
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request

REPO = 'aiprofesserzhaofeng8-jpg/codearts2api-panel'
API = 'https://api.github.com/repos/' + REPO + '/releases/'
BASE = 'https://github.com/' + REPO + '/releases/download/'

class InstallError(Exception):
    pass

def require(ok, message):
    if not ok:
        raise InstallError(message)

def version(value):
    require(re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+', value), '仅支持稳定版本 vX.Y.Z')
    return value

class Redirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        require(not req.full_url.startswith(API), '版本 API 不允许重定向')
        parsed = urllib.parse.urlsplit(newurl)
        require(parsed.scheme == 'https' and parsed.hostname in ('github.com', 'release-assets.githubusercontent.com', 'objects.githubusercontent.com') and parsed.port in (None, 443) and not parsed.username, '下载重定向来源无效')
        return super().redirect_request(req, fp, code, msg, headers, newurl)

def fetch(url, target, limit):
    request = urllib.request.Request(url, headers={'User-Agent': 'codearts2api-installer', 'Accept': 'application/vnd.github+json' if url.startswith(API) else 'application/octet-stream'})
    if url.startswith(API) and os.environ.get('GITHUB_TOKEN'):
        request.add_header('Authorization', 'Bearer ' + os.environ['GITHUB_TOKEN'])
    with urllib.request.build_opener(Redirects()).open(request, timeout=30) as source, target.open('xb') as output:
        size = 0
        while chunk := source.read(1024 * 1024):
            size += len(chunk)
            require(size <= limit, '下载超出大小限制')
            output.write(chunk)

def release(work, selected):
    meta = work / 'release.json'
    fetch(API + ('latest' if selected == 'latest' else 'tags/' + version(selected)), meta, 2 * 1024**2)
    data = json.loads(meta.read_text())
    require(isinstance(data, dict) and not data.get('draft') and not data.get('prerelease'), '没有可用的正式发布')
    tag = version(data.get('tag_name', ''))
    require(selected == 'latest' or tag == selected, '返回版本不匹配')
    return tag, data

def assets(data, tag, required):
    result = {}
    for item in data.get('assets', []):
        name = item.get('name')
        if name not in required:
            continue
        require(name not in result, '发布附件重复')
        url = BASE + tag + '/' + name
        require(item.get('browser_download_url') == url, '发布附件来源无效')
        result[name] = url
    require(set(result) == set(required), '该版本缺少安装附件，尚不能安装或升级')
    return result

def checksums(path):
    result = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r'([a-fA-F0-9]{64}) [ *]([A-Za-z0-9_.-]+)', line)
        require(match is not None and match[2] not in result, '校验清单无效或重复')
        result[match[2]] = match[1].lower()
    return result

def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            h.update(chunk)
    return h.hexdigest()

def main():
    require(sys.version_info >= (3, 10), '需要 Python 3.10 或更新版本')
    p = argparse.ArgumentParser(description='CodeArts2API 稳定版本安装入口；升级前请阅读发布说明。离线使用 panelctl.py。')
    p.add_argument('command', nargs='?', choices=['check', 'download', 'install', 'upgrade'])
    p.add_argument('--version', default='latest')
    p.add_argument('--root', type=Path)
    p.add_argument('--output', type=Path)
    p.add_argument('--name', default='managed')
    p.add_argument('--port', type=int, default=10087)
    p.add_argument('--system', action='store_true', help='安装系统级服务；无参数时默认启用')
    p.add_argument('--host', choices=['127.0.0.1', '0.0.0.0'], default='127.0.0.1')
    args = p.parse_args()
    args.system = args.system or args.command is None or (args.command in ('install', 'upgrade') and args.root is None and os.geteuid() == 0)
    args.command = args.command or 'install'
    if args.system and args.command in ('install', 'upgrade'):
        require(os.geteuid() == 0, '系统级安装/升级需要 root，请使用 curl … | sudo bash')
        args.root = args.root or Path('/opt/codearts2api')
        require(Path('/run/systemd/system').is_dir() and shutil.which('systemctl'), '需要正在运行的 systemd 系统服务管理器')
        if args.command == 'install':
            require(not args.root.exists() and not args.root.is_symlink(), '安装目录已存在；请使用 upgrade，未覆盖已有数据')
    require(platform.system() == 'Linux', '此安装入口仅支持 Linux')
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(arch, '此安装入口仅支持 amd64/arm64')
    require(args.command not in ('install', 'upgrade') or args.root is not None, '安装/升级需要 --root')
    require(args.command != 'download' or args.output is not None, '下载需要 --output 指定尚不存在的目录')
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix='codearts-release-') as temp:
        work = Path(temp)
        tag, data = release(work, args.version)
        name = f'codearts2api-panel-{tag}-linux-{arch}.tar.gz'
        urls = assets(data, tag, [name, 'panelctl.py', 'SHA256SUMS'])
        print('稳定版本：' + tag, flush=True)
        if args.command == 'check':
            print('发布附件齐全；安装时将继续校验摘要和状态版本。')
            return
        fetch(urls['SHA256SUMS'], work / 'SHA256SUMS', 65536)
        expected = checksums(work / 'SHA256SUMS')
        for item in (name, 'panelctl.py'):
            require(item in expected, '校验清单缺少必需附件')
            fetch(urls[item], work / item, 512 * 1024**2 if item == name else 2 * 1024**2)
            require(digest(work / item) == expected[item], '附件摘要不匹配，未执行安装')
        if args.command == 'download':
            # Exclusive directory creation avoids replacing previous downloads.
            args.output.mkdir(mode=0o700)
            for item in (name, 'panelctl.py', 'SHA256SUMS'):
                (args.output / item).write_bytes((work / item).read_bytes())
            print('已校验并下载至：' + str(args.output))
            return
        command = [sys.executable, str(work / 'panelctl.py'), args.command, '--root', str(args.root), '--archive', str(work / name), '--sha256', expected[name]]
        if args.command == 'install':
            command += ['--name', args.name, '--port', str(args.port)]
            if args.system:
                capability = subprocess.run([sys.executable, str(work / 'panelctl.py'), 'install', '--help'], capture_output=True, text=True, check=True)
                require('--system' in capability.stdout, '该正式版本尚不支持系统级安装，请等待包含新安装工具的版本发布')
                command += ['--system', '--host', args.host]
            else:
                require(args.host == '127.0.0.1', '自定义监听地址请使用 --system')
        subprocess.run(command, check=True)

if __name__ == '__main__':
    try:
        main()
    except urllib.error.HTTPError as error:
        print('GitHub 拒绝或限制请求；可稍后重试，或在服务器配置 GITHUB_TOKEN。' if error.code in (403,429) else '发布版本或附件不可用，请检查版本号。', file=sys.stderr)
        sys.exit(1)
    except Exception as error:
        print(str(error) if isinstance(error, InstallError) else '获取或安装失败；请检查网络、发布附件和部署状态。', file=sys.stderr)
        sys.exit(1)
PY
