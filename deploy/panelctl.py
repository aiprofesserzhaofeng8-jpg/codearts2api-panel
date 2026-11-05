#!/usr/bin/env python3
"""Offline single-writer deployment operations. Python >=3.10, Linux, stdlib only.

Never imported by the gateway. Archives are private, checksummed, not encrypted.
"""
import argparse
from contextlib import contextmanager, ExitStack
import ctypes
import errno
import fcntl
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import platform
import pwd
import grp
import re
import shutil
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

MAX_BYTES = 8 * 1024**3
MAX_FILES = 100000
SIDECARS = ('.usage.jsonl', '.growth.json', '.installed', '.access.json',
            '.admin-recovery.json', '.setup.json')


class OpsError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise OpsError(message)


def sha256(path):
    digest = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def state_schema(path):
    value = json.loads(Path(path).read_text())
    require(isinstance(value, dict), '主状态必须是 JSON 对象')
    schema = value.get('version', 0)
    require(type(schema) is int and schema >= 0, '主状态版本无效')
    return schema


def private_json(path, value):
    with open(path, 'x', encoding='utf-8') as stream:
        os.chmod(path, 0o600)
        json.dump(value, stream, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())


def replace_private_json(path, value):
    candidate = path.with_name(path.name + f'.{time.time_ns()}.tmp')
    private_json(candidate, value)
    os.replace(candidate, path)
    sync_dir(path.parent)


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def publish_directory(source, target):
    """Linux atomic no-replace rename, including an existing empty directory."""
    try:
        rename = ctypes.CDLL(None, use_errno=True).renameat2
    except AttributeError:
        raise OpsError('系统缺少 renameat2，无法保证恢复不覆盖；请使用受支持的 Linux 主机') from None
    rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    rename.restype = ctypes.c_int
    # AT_FDCWD=-100, RENAME_NOREPLACE=1; no unsafe check-then-rename fallback.
    if rename(-100, os.fsencode(source), -100, os.fsencode(target), 1) != 0:
        code = ctypes.get_errno()
        if code == errno.EEXIST:
            raise OpsError('恢复目标已经存在，未覆盖任何文件')
        if code in (errno.ENOSYS, errno.EINVAL):
            raise OpsError('内核或文件系统不支持原子无覆盖恢复')
        raise OSError(code, os.strerror(code))


def run(command, *, check=True):
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, timeout=90)
    if check:
        require(result.returncode == 0, f'命令失败：{command[0]}（退出 {result.returncode}）；请检查服务日志')
    return result


def safe_name(name):
    path = PurePosixPath(name)
    return (bool(name) and not path.is_absolute() and '\\' not in name
            and all(x not in ('', '.', '..') for x in name.split('/')))


def inventory(directory, *, digest=True):
    directory = Path(directory)
    require(directory.is_dir() and not directory.is_symlink(), '需要真实的专用数据目录')
    files = {}
    total = 0
    def failed(error):
        raise error
    for base, dirs, names in os.walk(directory, followlinks=False, onerror=failed):
        for name in dirs:
            require(not (Path(base) / name).is_symlink(), '目录包含符号链接，拒绝归档')
        for name in names:
            path = Path(base) / name
            require(stat.S_ISREG(path.lstat().st_mode), '目录包含链接或特殊文件，拒绝归档')
            rel = path.relative_to(directory).as_posix()
            require(safe_name(rel), '文件名不安全')
            size = path.stat().st_size
            total += size
            require(total <= MAX_BYTES and len(files) < MAX_FILES, '超出归档大小或文件数量限制')
            files[rel] = {'size': size, 'sha256': sha256(path) if digest else None}
    return files


def snapshot_schema(directory, files, state_name):
    require(isinstance(state_name, str) and safe_name(state_name), '状态文件名无效')
    if state_name in files:
        return state_schema(Path(directory) / state_name)
    require(state_name + '.setup.json' in files, '未找到状态或首次安装凭证；请指定正确 --state-name')
    require(not any(state_name + suffix in files for suffix in SIDECARS if suffix != '.setup.json'),
            '主状态缺失但仍有安装、恢复或账本文件；请恢复完整状态')
    return None


@contextmanager
def operation_lock(directory):
    """Serialize this tool's operations; application writers still must be stopped."""
    path = Path(directory).resolve()
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(path.parent / ('.' + path.name + '.panelops.lock'),
                 os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise OpsError('该目录已有运维操作，请等待完成后重试') from None
        yield
    finally:
        os.close(fd)


def no_writer(directory):
    """Best-effort local detection; cannot replace stopping a supervisor or remote writer."""
    directory = Path(directory).resolve()
    require(Path('/proc').is_dir(), '仅支持 Linux；必须能检查本机进程')
    for proc in Path('/proc').iterdir():
        if not proc.name.isdigit() or int(proc.name) == os.getpid():
            continue
        try:
            args = (proc / 'cmdline').read_bytes().split(b'\0')
            text_args = [x.decode(errors='replace') for x in args if x]
            for i, arg in enumerate(text_args):
                candidate = None
                if arg in ('-data', '--data') and i + 1 < len(text_args):
                    candidate = text_args[i + 1]
                elif arg.startswith(('-data=', '--data=')):
                    candidate = arg.split('=', 1)[1]
                if candidate:
                    path = Path(candidate)
                    if not path.is_absolute():
                        path = (proc / 'cwd').resolve() / path
                    if path.resolve().is_relative_to(directory):
                        raise OpsError('数据目录仍有进程使用；请先停止实际写者及其自动重启管理器')
            for fd in (proc / 'fd').iterdir():
                try:
                    target = fd.resolve(strict=True)
                    if target.is_relative_to(directory) and not target.is_dir():
                        # Shells/file browsers may hold directory descriptors. They cannot
                        # write file contents through those descriptors; retain regular-file
                        # and explicit application state-path checks above.
                        raise OpsError('数据目录仍被进程打开，拒绝离线操作')
                except (FileNotFoundError, PermissionError):
                    pass
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            continue


class Service:
    def __init__(self, unit=None, compose=None, name='codearts2api', system=False):
        require(bool(unit) != bool(compose), '必须指定 systemd unit 或 Compose 文件')
        self.unit, self.compose, self.name = unit, compose, name
        if unit:
            require(re.fullmatch(r'[a-zA-Z0-9_.@-]+\.service', unit) is not None, 'unit 名称无效')
        else:
            require(re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]*', name) is not None, 'Compose 服务名称无效')
        self.prefix = (['systemctl'] if system else ['systemctl', '--user']) if unit else ['docker', 'compose', '-f', str(Path(compose).resolve())]

    def running(self):
        if self.unit:
            result = run(self.prefix + ['show', self.unit, '-p', 'ActiveState', '--value'])
            state = result.stdout.strip()
            require(state in ('active', 'inactive', 'failed'), '服务状态正在变化或未知，请稍后重试')
            return state == 'active'
        raw = run(self.prefix + ['ps', '--all', '--format', 'json', self.name]).stdout.strip()
        # Compose versions emit either a JSON array or one object per line.
        containers = json.loads(raw) if raw.startswith('[') else [json.loads(line) for line in raw.splitlines()]
        require(isinstance(containers, list) and containers, '未找到指定 Compose 实例，请核对项目和服务名')
        require(all(isinstance(c, dict) and c.get('State') in ('running', 'exited', 'created') for c in containers),
                'Compose 实例重启中、暂停或状态未知；拒绝备份，请先排查')
        return any(c['State'] == 'running' for c in containers)

    def stop(self):
        run(self.prefix + (['stop', self.unit] if self.unit else ['stop', '-t', '30', self.name]))
        require(not self.running(), '服务未停止，拒绝读取状态')

    def start(self):
        run(self.prefix + (['start', self.unit] if self.unit else ['start', self.name]))


@contextmanager
def stopped(directory, service=None, offline=False):
    require(service is not None or offline, '请指定服务，或在停止所有写者后显式使用 --offline')
    was_running = service.running() if service else False
    try:
        if service:
            service.stop()
        no_writer(directory)
        yield
    finally:
        if was_running:
            service.start()


def backup(directory, archive, state_name='state.json'):
    require(not Path(directory).is_symlink(), '数据目录不能是符号链接')
    directory, archive = Path(directory).resolve(), Path(archive).absolute()
    require(not archive.resolve().is_relative_to(directory), '备份必须位于数据目录之外')
    require(not archive.exists() and not archive.is_symlink(), '备份目标已存在')
    files = inventory(directory)
    schema = snapshot_schema(directory, files, state_name)
    manifest = {'format': 1, 'kind': 'backup', 'schema': schema,
                'state_name': state_name, 'created_at': int(time.time()), 'files': files}
    archive.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, temp = tempfile.mkstemp(prefix='.backup-', dir=archive.parent)
    try:
        with os.fdopen(fd, 'wb') as output:
            with tarfile.open(fileobj=output, mode='w:gz') as tar:
                data = json.dumps(manifest).encode()
                info = tarfile.TarInfo('manifest.json')
                info.size, info.mode = len(data), 0o600
                tar.addfile(info, io.BytesIO(data))
                for name in sorted(files):
                    tar.add(directory / name, arcname='data/' + name, recursive=False)
            output.flush()
            os.fsync(output.fileno())
        require(inventory(directory) == files, '备份期间数据发生变化，拒绝生成不一致归档')
        # Validate the archived bytes too, not just before/after file fingerprints.
        with tempfile.TemporaryDirectory(dir=archive.parent) as check:
            unpack(temp, Path(check) / 'verified', expected_kind='backup')
        os.link(temp, archive)  # atomic no-overwrite publication
        sync_dir(archive.parent)
    finally:
        Path(temp).unlink(missing_ok=True)
    return manifest


def unpack(archive, target, expected_kind, expected_sha=None):
    """Validate completely into a sibling staging directory; never extractall or overwrite."""
    archive, target = Path(archive), Path(target).absolute()
    require(not target.exists() and not target.is_symlink(), '目标必须是不存在的新目录；禁止覆盖现有状态或凭据')
    if expected_sha is not None:
        require(re.fullmatch(r'[0-9a-fA-F]{64}', expected_sha) is not None, 'SHA-256 格式无效')
        require(sha256(archive) == expected_sha.lower(), '发布包 SHA-256 不匹配')
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    staging = Path(tempfile.mkdtemp(prefix='.restore-', dir=target.parent))
    try:
        with tarfile.open(archive, 'r:gz') as tar:
            members, total = {}, 0
            for member in tar:
                require(member.isfile() and safe_name(member.name), '归档含非普通文件或不安全路径')
                require(member.name not in members, '归档有重复路径')
                total += member.size
                require(total <= MAX_BYTES and len(members) < MAX_FILES + 1, '归档超出大小或数量限制')
                members[member.name] = member
            require('manifest.json' in members and members['manifest.json'].size <= 16 * 1024**2, '缺少有效清单')
            manifest = json.load(tar.extractfile(members['manifest.json']))
            require(isinstance(manifest, dict), '清单必须是对象')
            require(manifest.get('format') == 1 and manifest.get('kind') == expected_kind, '归档类型或格式不兼容')
            files = manifest.get('files')
            require(isinstance(files, dict) and files, '清单为空或无效')
            require(set(members) == {'manifest.json'} | {'data/' + x for x in files}, '清单与归档文件集合不一致')
            for name, entry in files.items():
                require(safe_name(name), '清单路径不安全')
                require(isinstance(entry, dict), '清单条目必须是对象')
                member = members['data/' + name]
                require(entry['size'] == member.size, '文件大小不匹配')
                path = staging / name
                path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                with path.open('xb') as output, tar.extractfile(member) as source:
                    os.chmod(path, 0o600)
                    shutil.copyfileobj(source, output, 1024 * 1024)
                    output.flush()
                    os.fsync(output.fileno())
                require(sha256(path) == entry['sha256'], '文件摘要不匹配')
        # Preserve the checked metadata for a release, but not inside restored data.
        if expected_kind == 'release':
            private_json(staging / 'manifest.json', manifest)
            os.chmod(staging / 'codearts2api-panel', 0o700)
        else:
            schema = snapshot_schema(staging, files, manifest.get('state_name'))
            declared = manifest.get('schema')
            require(declared is None or type(declared) is int, '归档状态版本无效')
            require(schema == declared, '归档清单与主状态版本不一致')
        for base, _, _ in os.walk(staging, topdown=False):
            sync_dir(base)
        publish_directory(staging, target)
        sync_dir(target.parent)
        return manifest
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def json_get(url):
    # Never use ambient proxy credentials for a local health request; never follow redirects.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(url, timeout=3) as response:
        value = json.loads(response.read(65536))
        return value if isinstance(value, dict) else {}


def health(base):
    try:
        value = json_get(base.rstrip('/') + '/health')
        if value.get('service') == 'codearts2api-panel' and value.get('status') == 'ok':
            return 'running'
    except (OSError, ValueError):
        pass
    try:
        value = json_get(base.rstrip('/') + '/setup/status')
        if value.get('needs_setup') is True and value.get('ready') is False:
            return 'setup_required'
    except (OSError, ValueError):
        pass
    return 'unhealthy'


def doctor(args):
    directory = Path(args.data_dir).absolute()
    findings = []
    schema = None
    if directory.is_dir():
        if directory.stat().st_mode & 0o077:
            findings.append('数据目录应仅允许服务用户访问（0700）')
        try:
            files = inventory(directory, digest=False)
            try:
                schema = snapshot_schema(directory, files, args.state_name)
            except OpsError as error:
                findings.append(str(error))
            for base, _, _ in os.walk(directory):
                if Path(base) != directory and Path(base).stat().st_mode & 0o077:
                    findings.append('存在其他用户可访问的子目录；检查权限')
                    break
            for name in files:
                if (directory / name).stat().st_mode & 0o077:
                    findings.append('存在其他用户可读取的文件；检查权限')
                    break
        except (OpsError, OSError, ValueError):
            findings.append('数据文件不可读取、格式错误或含特殊文件')
    else:
        findings.append('数据目录不存在')
    status = health(args.url)
    print(json.dumps({'status': status, 'schema': schema, 'findings': findings}, ensure_ascii=False))
    return 1 if findings or status == 'unhealthy' else 0


def release_version(value):
    match = re.fullmatch(r'v?(\d+)\.(\d+)\.(\d+)', str(value))
    require(match is not None, '首批部署工具只接受明确的稳定版本号 vX.Y.Z')
    return tuple(map(int, match.groups()))


def check_release(path):
    meta = json.loads((path / 'manifest.json').read_text())
    release_version(meta.get('version'))
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    require(meta.get('os') == 'linux' and meta.get('arch') == arch, '发布包与当前操作系统/架构不匹配')
    require(type(meta.get('schema')) is int and meta['schema'] >= 0, '发布包未声明有效状态版本')
    result = run([str(path / 'codearts2api-panel'), '-version'])
    require(result.stdout.strip() == 'codearts2api ' + meta['version'], '二进制版本与包清单不一致')
    return meta


def unit_text(root, port, no_refresh=False, system_user=None, host='127.0.0.1'):
    # Restrict generated paths to avoid systemd specifier/quoting ambiguity.
    require(re.fullmatch(r'/[a-zA-Z0-9_./-]+', str(root)) is not None, '安装路径只支持 ASCII 字母、数字、点、横线、下划线和斜线')
    require(host in ('127.0.0.1', '0.0.0.0'), '监听地址仅支持 127.0.0.1 或 0.0.0.0')
    identity = ''
    if system_user:
        require(re.fullmatch(r'[a-z_][a-z0-9_-]{0,30}', system_user) is not None, '系统服务用户无效')
        identity = f'''User={system_user}
Group={system_user}
Environment=HOME={root}/data
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths={root}/data
CapabilityBoundingSet=
RestrictSUIDSGID=true
'''
    return f'''[Unit]
Description=CodeArts2API managed installation
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
{identity}\
WorkingDirectory={root}
ExecStart={root}/current/codearts2api-panel -host {host} -port {port} -data {root}/data/state.json -no-import{ " -no-refresh" if no_refresh else "" }
Restart=on-failure
RestartSec=3
TimeoutStopSec=30
UMask=0077
[Install]
WantedBy={"multi-user.target" if system_user else "default.target"}
'''


def wait_health(url, allow_setup=False):
    for _ in range(30):
        status = health(url)
        if status == 'running' or (allow_setup and status == 'setup_required'):
            return status
        time.sleep(1)
    raise OpsError('启动检查失败；保留当前数据与版本，请检查 journalctl，不自动恢复旧数据')


def verify_process(unit, binary, system=False):
    prefix = ['systemctl'] if system else ['systemctl', '--user']
    pid = run(prefix + ['show', unit, '-p', 'MainPID', '--value']).stdout.strip()
    require(pid.isdigit() and int(pid) > 0 and (Path('/proc') / pid / 'exe').resolve() == binary,
            '健康接口正常但实际进程不是目标版本')


def system_root(root):
    require(os.geteuid() == 0, '系统级安装/升级需要 root，请使用 sudo')
    require(root.is_absolute() and root == root.resolve(), '系统安装路径不能含符号链接或相对路径')
    require(root != Path('/'), '不能安装到文件系统根目录')
    require(root.parent.is_dir(), '系统安装路径的父目录必须预先存在')
    if root.exists():
        info = root.stat()
        require(info.st_uid == 0 and not info.st_mode & 0o022, '系统安装目录必须由 root 持有且其他用户不可写')
    for parent in root.parents:
        if parent.exists():
            info = parent.stat()
            require(info.st_uid == 0 and not info.st_mode & 0o022,
                    '系统安装路径的父目录必须由 root 持有且其他用户不可写')


def service_account(name, root):
    shell = next((p for p in ('/usr/sbin/nologin', '/sbin/nologin') if Path(p).is_file()), None)
    require(shell is not None and shutil.which('useradd'), '系统安装需要 useradd 和 nologin')
    try:
        account = pwd.getpwnam(name)
    except KeyError:
        run(['useradd', '--system', '--user-group', '--no-create-home', '--home-dir', str(root / 'data'), '--shell', shell, name])
        account = pwd.getpwnam(name)
    require(account.pw_uid != 0 and account.pw_dir == str(root / 'data') and account.pw_shell == shell,
            '同名用户不是此安装的专用服务用户，拒绝复用')
    require(grp.getgrnam(name).gr_gid == account.pw_gid, '服务用户组不匹配')
    return account


def system_release(path, gid):
    # Program and deployment metadata stay root-owned; the daemon cannot replace them.
    for base, dirs, names in os.walk(path):
        os.chown(base, 0, gid)
        os.chmod(base, 0o750)
        for name in names:
            item = Path(base) / name
            require(item.is_file() and not item.is_symlink(), '程序目录含特殊文件')
            os.chown(item, 0, gid)
            os.chmod(item, 0o750 if name == 'codearts2api-panel' else 0o640)


def install(args):
    root = Path(args.root).absolute()
    system = getattr(args, 'system', False)
    if system:
        system_root(root)
        require(Path('/run/systemd/system').is_dir(), '需要正在运行的 systemd 系统服务管理器')
    require(not root.exists() and not root.is_symlink(), '安装目录已存在；请使用 upgrade 或另选目录')
    require(re.fullmatch(r'[a-zA-Z0-9_-]+', args.name) is not None, '实例名无效')
    require(1 <= args.port <= 65535, '端口无效')
    unit = 'codearts2api.service' if system and args.name == 'managed' else 'codearts2api-' + args.name + '.service'
    user = unit.removesuffix('.service') if system else None
    unit_dir = Path('/etc/systemd/system') if system else Path(os.environ.get('XDG_CONFIG_HOME', str(Path.home() / '.config'))) / 'systemd/user'
    unit_path = unit_dir / unit
    require(not unit_path.exists() and not unit_path.is_symlink(), '同名服务已存在，拒绝覆盖')
    if system:
        require(run(['systemctl', 'show', unit, '-p', 'LoadState', '--value']).stdout.strip() == 'not-found', '同名系统服务已存在，拒绝覆盖')
    host = getattr(args, 'host', '127.0.0.1')
    text = unit_text(root, args.port, getattr(args, "no_refresh", False), user, host)
    if not args.no_start:
        with socket.socket() as probe:
            try:
                probe.bind(('127.0.0.1', args.port))
            except OSError:
                raise OpsError('安装端口已被占用，尚未创建服务；请选择其他端口') from None
    root.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Fully validate before publishing a service or creating an installation.
    with tempfile.TemporaryDirectory(dir=root.parent) as temp:
        candidate = Path(temp) / 'candidate'
        unpack(args.archive, candidate, 'release', args.sha256)
        meta = check_release(candidate)
        account = service_account(user, root) if system else None
        root.mkdir(mode=0o700)
        (root / 'releases').mkdir(mode=0o700)
        (root / 'data').mkdir(mode=0o700)
        (root / 'backups').mkdir(mode=0o700)
        dest = root / 'releases' / meta['version']
        shutil.move(candidate, dest)
    if system:
        for directory in (root, root / 'releases'):
            os.chown(directory, 0, account.pw_gid)
            os.chmod(directory, 0o750)
        system_release(dest, account.pw_gid)
        os.chown(root / 'data', account.pw_uid, account.pw_gid)
    (root / 'current').symlink_to(dest.relative_to(root))
    private_json(root / 'installation.json', {'unit': unit, 'port': args.port, 'scope': 'system' if system else 'user', 'host': host})
    unit_dir.mkdir(parents=True, exist_ok=True)
    with unit_path.open('x') as output:
        output.write(text)
    os.chmod(unit_path, 0o644)
    if not args.no_start:
        service = Service(unit=unit, system=system)
        run(service.prefix + ['daemon-reload'])
        run(service.prefix + ['enable', unit])
        try:
            service.start()
            wait_health(f'http://127.0.0.1:{args.port}', allow_setup=True)
            verify_process(unit, dest / 'codearts2api-panel', system=system)
        except Exception:
            service.stop()
            raise
    print('版本与服务配置已准备。' if args.no_start else '安装完成；打开面板设置管理员邮箱和密码。')
    print(f'访问地址：http://127.0.0.1:{args.port}（服务器本机）')
    print(f'服务状态：{"sudo systemctl" if system else "systemctl --user"} status {unit}')


def model_migration_precheck(binary, state, source_schema=10):
    result = subprocess.run([str(binary), '-check-state-migration', str(state)],
                            capture_output=True, text=True, timeout=120)
    require(result.returncode in (0, 2), '候选程序状态预检查失败；未修改数据')
    try:
        report = json.loads(result.stdout)
    except ValueError:
        raise OpsError('迁移预检查返回格式无效') from None
    require(report.get('from_schema') == source_schema and report.get('to_schema') == 11,
            '候选程序迁移范围不匹配')
    require(result.returncode == 0 and report.get('ready') is True and report.get('conflicts') == [],
            '统一模型迁移存在冲突；请先运行候选程序 -check-state-migration 查看报告')
    return report


def model_migration_copy(binary, source, target):
    result = subprocess.run([str(binary), '-migrate-state-dir', str(source),
                             '-migration-output-dir', str(target)],
                            capture_output=True, text=True, timeout=300)
    require(result.returncode == 0, '隔离数据迁移失败；保留原数据和候选目录')
    require(state_schema(target / 'state.json') == 11 and
            not (target / '.model-migration.pending.json').exists(), '迁移未完整提交')


def upgrade(args):
    root = Path(args.root).absolute()
    migration_marker = root / 'schema-migration.json'
    require(not migration_marker.exists() and not migration_marker.is_symlink(),
            '上次 schema 迁移尚未完成；保留数据，请按迁移记录恢复后再升级')
    config = json.loads((root / 'installation.json').read_text())
    require(config.get('scope', 'user') in ('user', 'system'), '安装记录的服务 scope 无效')
    system = config.get('scope', 'user') == 'system'
    if system:
        system_root(root)
    service = Service(unit=config['unit'], system=system)
    require(service.running(), '升级要求受管服务当前正在运行；请先排查停止或失败原因')
    old = (root / 'current').resolve()
    previous = check_release(old)
    with tempfile.TemporaryDirectory(dir=root / 'releases') as temp:
        candidate = Path(temp) / 'candidate'
        unpack(args.archive, candidate, 'release', args.sha256)
        meta = check_release(candidate)
        if system:
            account = service_account(config['unit'].removesuffix('.service'), root)
            system_release(candidate, account.pw_gid)
        require(release_version(meta['version']) != release_version(previous['version']),
                f'当前已安装 {previous["version"]}，发布源没有更新版本；未停止服务或修改数据')
        require(release_version(meta['version']) > release_version(previous['version']), '只允许升级到更高版本；不自动降级')
        migrating = meta['schema'] != previous['schema']
        if migrating:
            require(getattr(args, 'migrate_schema', False) and previous['schema'] == 10 and meta['schema'] == 11
                    and {'from': 10, 'to': 11} in meta.get('state_migrations', []),
                    '跨 schema 升级需受支持的迁移包及显式 --migrate-schema；未停止服务')
            model_migration_precheck(candidate / 'codearts2api-panel', root / 'data/state.json')
        dest = root / 'releases' / meta['version']
        require(not dest.exists() and not dest.is_symlink(), '候选版本目录已存在，请先排查上次升级结果')
        link = root / 'current.next'
        require(not link.exists() and not link.is_symlink(), '存在未处理的版本切换文件')
        verify_process(config['unit'], old / 'codearts2api-panel', system=system)
        # Preflight before outage; recheck the actual state after stopping.
        switched = False
        data_moved = False
        migration_output = root / f'data-schema11-{time.time_ns()}'
        original_data = root / f'data-before-schema11-{time.time_ns()}'
        try:
            service.stop()
            no_writer(root / 'data')
            require(state_schema(root / 'data/state.json') == previous['schema'], '实际状态 schema 不兼容，拒绝升级')
            if migrating:
                model_migration_precheck(candidate / 'codearts2api-panel', root / 'data/state.json')
            archive = root / 'backups' / f'before-{meta["version"]}-{time.time_ns()}.tar.gz'
            backup(root / 'data', archive)
            if migrating:
                model_migration_copy(candidate / 'codearts2api-panel', root / 'data', migration_output)
                if system:
                    for entry in [migration_output, *migration_output.rglob('*')]:
                        require(not entry.is_symlink(), '迁移输出不能包含链接')
                        os.chown(entry, account.pw_uid, account.pw_gid)
                private_json(migration_marker, {'from': 10, 'to': 11, 'phase': 'prepared',
                             'old_release': str(old), 'new_release': str(dest),
                             'original_data': str(original_data), 'candidate_data': str(migration_output),
                             'backup': str(archive)})
                sync_dir(root)
            publish_directory(candidate, dest)
            sync_dir(dest.parent)
            require(not link.exists() and not link.is_symlink(), '存在未处理的版本切换文件')
            link.symlink_to(dest.relative_to(root))
            if migrating:
                # These renames are separate crash points. The marker guards all
                # future tool operations until successful startup is confirmed.
                (root / 'data').rename(original_data)
                data_moved = True
                migration_output.rename(root / 'data')
                sync_dir(root)
            os.replace(link, root / 'current')
            switched = True
            sync_dir(root)
            if migrating:
                record = json.loads(migration_marker.read_text())
                record['phase'] = 'starting'
                replace_private_json(migration_marker, record)
                if system:
                    os.chown(migration_marker, 0, account.pw_gid)
                    os.chmod(migration_marker, 0o640)
            service.start()
            wait_health(f'http://127.0.0.1:{config["port"]}')
            verify_process(config['unit'], dest / 'codearts2api-panel', system=system)
            if migrating:
                require(state_schema(root / 'data/state.json') == 11, '启动后的状态版本不匹配')
                migration_marker.rename(root / f'schema-migration-complete-{time.time_ns()}.json')
                sync_dir(root)
        except Exception:
            if switched or data_moved:
                service.stop()  # preserve newest credentials/ledger; never restore backup automatically
            else:
                if migration_marker.exists():
                    migration_marker.rename(root / f'schema-migration-aborted-{time.time_ns()}.json')
                service.start()
            raise
    print('升级完成；保留旧程序和升级前备份，未覆盖旧凭据或账本。')


def recover_schema(args):
    """Resume the recorded 10->11 transition; never restore older state."""
    root = Path(args.root).absolute()
    require(root == root.resolve(), '恢复路径不能包含符号链接')
    marker = root / 'schema-migration.json'
    if not marker.exists():
        require(not marker.is_symlink(), '迁移标记无效')
        print('没有待处理的 schema 迁移；未修改文件或服务。')
        return
    require(marker.is_file() and not marker.is_symlink(), '迁移标记无效')
    record = json.loads(marker.read_text())
    require(record.get('from') == 10 and record.get('to') == 11 and record.get('phase') in ('prepared', 'starting'),
            '不支持或无法识别的迁移阶段；未修改数据')
    paths = {}
    for key, parent, pattern in (
            ('old_release', root / 'releases', r'v?\d+\.\d+\.\d+'),
            ('new_release', root / 'releases', r'v?\d+\.\d+\.\d+'),
            ('original_data', root, r'data-before-schema11-\d+'),
            ('candidate_data', root, r'data-schema11-\d+')):
        path = Path(record[key])
        require(path.is_absolute() and path.parent == parent and re.fullmatch(pattern, path.name)
                and path.resolve() == path and not path.is_symlink(), '迁移记录的路径无效')
        paths[key] = path
    old, dest = paths['old_release'], paths['new_release']
    original, candidate = paths['original_data'], paths['candidate_data']
    data = root / 'data'
    require(old != dest and (root / 'current').is_symlink()
            and (root / 'current').resolve() in (old, dest), '当前版本不属于此迁移，拒绝猜测恢复')
    require(not data.is_symlink(), '数据目录不能是符号链接')
    config = json.loads((root / 'installation.json').read_text())
    require(config.get('scope', 'user') in ('user', 'system'), '安装记录的服务 scope 无效')
    system = config.get('scope', 'user') == 'system'
    if system:
        system_root(root)
    print(json.dumps({'phase': record['phase'], 'from': 10, 'to': 11,
                      'old_version': old.name, 'new_version': dest.name,
                      'data_schema': state_schema(data / 'state.json') if data.exists() else None,
                      'action': 'forward_only'}, ensure_ascii=False))
    if not args.apply:
        return
    # The exact archive is needed only if interruption preceded release publication.
    if not dest.exists():
        require(args.archive and args.sha256, '候选版本尚未发布；请提供原候选 --archive 和 --sha256')
        with tempfile.TemporaryDirectory(dir=root / 'releases') as temp:
            staging = Path(temp) / 'candidate'
            unpack(args.archive, staging, 'release', args.sha256)
            meta = check_release(staging)
            require(meta['version'] == dest.name and meta['schema'] == 11
                    and {'from': 10, 'to': 11} in meta.get('state_migrations', []), '恢复包与记录不匹配')
            if system:
                account = service_account(config['unit'].removesuffix('.service'), root)
                system_release(staging, account.pw_gid)
            publish_directory(staging, dest)
            sync_dir(dest.parent)
    require(check_release(dest)['schema'] == 11 and check_release(old)['schema'] == 10,
            '恢复版本的 schema 不匹配')
    link = root / 'current.next'
    require(not link.exists() and not link.is_symlink() or link.is_symlink() and link.resolve() == dest,
            '存在不属于此迁移的版本切换文件')
    service = Service(unit=config['unit'], system=system)
    if service.running():
        verify_process(config['unit'], (root / 'current').resolve() / 'codearts2api-panel', system=system)
    service.stop()
    try:
        for directory in (data, original, candidate):
            if directory.exists():
                no_writer(directory)
                inventory(directory, digest=False)
        if data.exists() and state_schema(data / 'state.json') == 10:
            require(not original.exists() and candidate.is_dir(), '升级前数据位置有歧义')
            model_migration_precheck(dest / 'codearts2api-panel', candidate / 'state.json', source_schema=11)
            require(state_schema(candidate / 'state.json') == 11
                    and not (candidate / '.model-migration.pending.json').exists(), '候选数据迁移不完整')
            publish_directory(data, original)
            sync_dir(root)
        if not data.exists():
            require(original.is_dir() and state_schema(original / 'state.json') == 10
                    and candidate.is_dir() and state_schema(candidate / 'state.json') == 11
                    and not (candidate / '.model-migration.pending.json').exists(), '迁移数据位置有歧义或不完整')
            model_migration_precheck(dest / 'codearts2api-panel', candidate / 'state.json', source_schema=11)
            publish_directory(candidate, data)
            sync_dir(root)
        require(state_schema(data / 'state.json') == 11 and original.is_dir() and not candidate.exists()
                and not (data / '.model-migration.pending.json').exists(), '当前数据不是完整的新版本；未覆盖任何数据')
        model_migration_precheck(dest / 'codearts2api-panel', data / 'state.json', source_schema=11)
        if not link.is_symlink():
            link.symlink_to(dest.relative_to(root))
        os.replace(link, root / 'current')
        sync_dir(root)
        record['phase'] = 'starting'
        replace_private_json(marker, record)
        if system:
            account = service_account(config['unit'].removesuffix('.service'), root)
            os.chown(marker, 0, account.pw_gid)
            os.chmod(marker, 0o640)
        service.start()
        wait_health(f'http://127.0.0.1:{config["port"]}')
        verify_process(config['unit'], dest / 'codearts2api-panel', system=system)
        require(state_schema(data / 'state.json') == 11, '启动后状态版本不匹配')
        marker.rename(root / f'schema-migration-complete-{time.time_ns()}.json')
        sync_dir(root)
    except Exception:
        service.stop()
        raise
    print('已继续完成 schema 迁移；保留最新数据与原目录，未恢复旧凭据或账本。')


def source_process(args):
    require(re.fullmatch(r'[A-Za-z0-9_.@-]+\.service', args.source_unit), '旧服务名无效')
    service = Service(unit=args.source_unit)
    require(service.running(), '旧服务必须处于运行状态')
    binary = Path(args.source_binary).resolve()
    verify_process(args.source_unit, binary)
    pid = run(['systemctl', '--user', 'show', args.source_unit, '-p', 'MainPID', '--value']).stdout.strip()
    proc = Path('/proc') / pid
    argv = (proc / 'cmdline').read_bytes().decode().rstrip('\0').split('\0')[1:]
    values, flags = {}, set()
    i = 0
    while i < len(argv):
        item = argv[i]
        if item in ('-no-import', '-no-refresh'):
            flags.add(item)
        elif item in ('-host', '-port', '-data') and i + 1 < len(argv):
            require(item not in values, '旧启动参数重复')
            i += 1
            values[item] = argv[i]
        else:
            raise OpsError('旧启动参数无法自动迁移；请按部署文档手工迁移')
        i += 1
    require(values.get('-host', '127.0.0.1') == '127.0.0.1', '仅支持 loopback 源码实例迁移')
    require(values.get('-port') == str(args.port), '新旧端口不一致')
    source = Path(args.data_dir).resolve()
    actual = Path(values.get('-data', ''))
    require(values.get('-data'), '旧服务未显式指定状态路径，拒绝猜测')
    if not actual.is_absolute():
        actual = (proc / 'cwd').resolve() / actual
    require(actual.resolve() == source / 'state.json', '旧进程的数据路径不匹配')
    require(args.no_refresh == ('-no-refresh' in flags), '需要显式保持旧服务的 --no-refresh 选择')
    # CLI/environment overrides outside the explicit migration contract are rejected.
    env = (proc / 'environ').read_bytes().split(b'\0')
    require(not any(x.startswith(b'CODEARTS_') for x in env), '旧服务含环境配置，请先改为明确受支持的启动参数')
    return service, pid


def migrate(args):
    root = Path(args.root).absolute()
    source = Path(args.data_dir).resolve()
    require(root != source and source not in root.parents and root not in source.parents, '源数据与目标目录必须独立')
    old, pid = source_process(args)
    # install(no_start) validates the whole candidate before touching the running instance.
    args.no_start = True
    install(args)
    cfg = json.loads((root / 'installation.json').read_text())
    new = Service(unit=cfg['unit'])
    run(['systemctl', '--user', 'daemon-reload'])
    launched = False
    disabled = False
    was_enabled = run(['systemctl', '--user', 'is-enabled', args.source_unit], check=False).stdout.strip() == 'enabled'
    try:
        require(source_process(args)[1] == pid, '旧进程已变化，拒绝迁移')
        old.stop()
        no_writer(source)
        meta = check_release((root / 'current').resolve())
        require(state_schema(source / 'state.json') == meta['schema'], '迁移仅支持相同状态版本')
        archive = root / 'backups' / f'before-migration-{time.time_ns()}.tar.gz'
        backup(source, archive)
        (root / 'data').rmdir()  # Only the empty directory created by this operation.
        unpack(archive, root / 'data', 'backup')
        run(['systemctl', '--user', 'disable', args.source_unit])
        disabled = True
        launched = True
        new.start()
        wait_health(f'http://127.0.0.1:{args.port}')
        verify_process(cfg['unit'], (root / 'current').resolve() / 'codearts2api-panel')
        run(['systemctl', '--user', 'enable', cfg['unit']])
    except Exception:
        if launched:
            new.stop()  # Candidate may have persisted rotated credentials: never restart old data.
        else:
            if disabled and was_enabled:
                run(['systemctl', '--user', 'enable', args.source_unit])
            old.start()
        raise
    print('迁移完成；旧服务已禁用。后续仅使用新目录；旧目录和备份保留，不可并行启动。')


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest='command', required=True)
    b = sub.add_parser('backup')
    b.add_argument('--data-dir', required=True)
    b.add_argument('--state-name', default='state.json')
    b.add_argument('--archive', required=True)
    mode = b.add_mutually_exclusive_group(required=True)
    mode.add_argument('--unit')
    mode.add_argument('--compose')
    mode.add_argument('--offline', action='store_true')
    b.add_argument('--service', default='codearts2api')
    b.add_argument('--system', action='store_true', help='使用系统级 systemd unit（需要 root）')
    r = sub.add_parser('restore')
    r.add_argument('--archive', required=True)
    r.add_argument('--target', required=True)
    d = sub.add_parser('doctor')
    d.add_argument('--data-dir', required=True)
    d.add_argument('--state-name', default='state.json')
    d.add_argument('--url', default='http://127.0.0.1:10087')
    h = sub.add_parser('health')
    h.add_argument('--url', default='http://127.0.0.1:10087')
    h.add_argument('--ready', action='store_true', help='初始化等待也返回非零，供流量就绪检查')
    recovery = sub.add_parser('recover-schema', help='检查或向前继续未完成的 10→11 迁移')
    recovery.add_argument('--root', required=True)
    recovery.add_argument('--apply', action='store_true', help='显式停写并向前恢复；默认只检查')
    recovery.add_argument('--archive')
    recovery.add_argument('--sha256')
    for name in ('install', 'upgrade', 'migrate'):
        cmd = sub.add_parser(name)
        cmd.add_argument('--archive', required=True)
        cmd.add_argument('--sha256', required=True)
        cmd.add_argument('--root', required=True)
        if name in ('install', 'migrate'):
            cmd.add_argument('--name', default='managed')
            cmd.add_argument('--port', type=int, default=10087)
            cmd.add_argument('--no-start', action='store_true')
            cmd.add_argument('--no-refresh', action='store_true')
        if name == 'install':
            cmd.add_argument('--system', action='store_true', help='安装系统级服务，需要 root')
            cmd.add_argument('--host', choices=['127.0.0.1', '0.0.0.0'], default='127.0.0.1')
        if name == 'upgrade':
            cmd.add_argument('--migrate-schema', action='store_true', help='显式启用发布包声明支持的 10→11 迁移')
        if name == 'migrate':
            cmd.add_argument('--source-unit', required=True)
            cmd.add_argument('--source-binary', required=True)
            cmd.add_argument('--data-dir', required=True)
    return p


def main():
    os.umask(0o077)
    args = parser().parse_args()
    try:
        if args.command == 'recover-schema':
            root = Path(args.root).absolute()
            require(root.is_dir() and root == root.resolve() and not (root / 'data').is_symlink(),
                    '恢复需要已存在的真实安装目录，数据不能是符号链接')
            if not args.apply:
                return dispatch(args)  # Inspection must not even create operation-lock files.
        if args.command == 'install' and args.system:
            system_root(Path(args.root).absolute())
        with ExitStack() as locks:
            if args.command == 'backup':
                locks.enter_context(operation_lock(args.data_dir))
            elif args.command == 'restore':
                locks.enter_context(operation_lock(args.target))
            elif args.command in ('install', 'upgrade', 'migrate', 'recover-schema'):
                locks.enter_context(operation_lock(args.root))
                if args.command == 'migrate':
                    locks.enter_context(operation_lock(args.data_dir))
                if args.command in ('upgrade', 'recover-schema'):
                    locks.enter_context(operation_lock(Path(args.root) / 'data'))
            return dispatch(args)
    except (OpsError, OSError, ValueError, KeyError, TypeError, tarfile.TarError, subprocess.TimeoutExpired) as error:
        # Arbitrary file/HTTP content must never reach stdout/stderr.
        print(str(error) if isinstance(error, OpsError) else '操作失败：文件、配置或外部命令无效；未自动回滚数据', file=sys.stderr)
        return 1


def dispatch(args):
    if args.command == 'backup':
        require(not args.system or (args.unit and os.geteuid() == 0), '--system 备份需要 root 和 --unit')
        service = Service(args.unit, args.compose, args.service, system=args.system) if args.unit or args.compose else None
        with stopped(args.data_dir, service, args.offline):
            manifest = backup(args.data_dir, args.archive, args.state_name)
        print(json.dumps({'files': len(manifest['files']), 'schema': manifest['schema'], 'sha256': sha256(args.archive)}))
    elif args.command == 'restore':
        unpack(args.archive, args.target, 'backup')
        print('校验并恢复到新目录；尚未启动服务。切换前确认没有更新的在线凭据/账本。')
    elif args.command == 'doctor':
        return doctor(args)
    elif args.command == 'health':
        status = health(args.url)
        print(status)
        return int(status == 'unhealthy' or (args.ready and status != 'running'))
    elif args.command == 'install':
        install(args)
    elif args.command == 'upgrade':
        upgrade(args)
    elif args.command == 'recover-schema':
        recover_schema(args)
    elif args.command == 'migrate':
        migrate(args)
    return 0


if __name__ == '__main__':
    sys.exit(main())
