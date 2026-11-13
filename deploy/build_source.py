"""Record source identity and rebuild UI from the same checkout as the backend."""
import hashlib
import subprocess


def prepare_source(root, version):
    def git(*args):
        return subprocess.check_output(['git', '-C', str(root), *args])
    commit = git('rev-parse', 'HEAD').decode().strip()
    dirty = bool(git('status', '--porcelain', '--untracked-files=normal'))
    if dirty and '-local.' not in version:
        raise RuntimeError('正式发布必须使用干净的已提交源码；本地验证请使用 vX.Y.Z-local.YYYYMMDDHHMMSS')
    digest = hashlib.sha256()
    # Hash source bytes without exposing paths, diffs or runtime state in the package.
    for name in sorted(set(git('ls-files', '-z', '--cached', '--others', '--exclude-standard').split(b'\0'))):
        if not name:
            continue
        path = root / name.decode()
        if path.is_file():
            digest.update(name + b'\0' + path.read_bytes() + b'\0')
    subprocess.check_call(['sh', str(root / 'scripts/frontend.sh'), 'build'], cwd=root)
    return {'commit': commit, 'dirty': dirty, 'sha256': digest.hexdigest(),
            'channel': 'local' if '-local.' in version else 'stable'}
