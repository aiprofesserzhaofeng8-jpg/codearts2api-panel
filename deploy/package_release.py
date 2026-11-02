#!/usr/bin/env python3
"""Build a checksummed Linux release from current sources and embedded UI."""
import argparse
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

from panelctl import inventory, release_version, require, sha256, sync_dir

ROOT = Path(__file__).resolve().parents[1]


def build(version, arch, output, go):
    release_version(version)
    require(arch in ('amd64', 'arm64'), '不支持的发布架构')
    schema_source = (ROOT / 'backend/internal/storage/store.go').read_text()
    schema = int(re.search(r'const StateVersion = (\d+)', schema_source)[1])
    # Caller (CI/build.sh/make frontend) must build assets from the same commit first.
    if not (ROOT / 'backend/internal/webui/dist/index.html').is_file():
        raise RuntimeError('先构建前端：make frontend')
    output.mkdir(parents=True, exist_ok=True)
    archive = output / f'codearts2api-panel-{version}-linux-{arch}.tar.gz'
    checksum = output / (archive.name + '.sha256')
    if any(p.exists() or p.is_symlink() for p in (archive, checksum)):
        raise RuntimeError('发布包已存在，拒绝覆盖')
    with tempfile.TemporaryDirectory(prefix='.package-', dir=output) as temp:
        staging = Path(temp) / 'data'
        staging.mkdir()
        subprocess.run([go, 'build', '-trimpath', '-ldflags', f'-s -w -X main.version={version}',
                        '-o', str(staging / 'codearts2api-panel'), './cmd/panel'], cwd=ROOT / 'backend',
                       env={**os.environ, 'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': arch}, check=True)
        for name in ('panelctl.py', 'install.sh', 'healthcheck.sh', 'Caddyfile', 'compose.production.yml', '.env.example', 'README.md'):
            (staging / name).write_bytes((ROOT / 'deploy' / name).read_bytes())
        for name in ('LICENSE', 'LICENSE.workbuddy'):
            (staging / name).write_bytes((ROOT / name).read_bytes())
        files = inventory(staging)
        meta = {'format': 1, 'kind': 'release', 'version': version, 'os': 'linux', 'arch': arch,
                'schema': schema, 'files': files}
        packed, summed = Path(temp) / 'package.tar.gz', Path(temp) / 'package.sha256'
        with packed.open('xb') as stream:
            with tarfile.open(fileobj=stream, mode='w:gz') as tar:
                payload = json.dumps(meta).encode()
                info = tarfile.TarInfo('manifest.json')
                info.size = len(payload)
                tar.addfile(info, io.BytesIO(payload))
                for name in sorted(files):
                    tar.add(staging / name, arcname='data/' + name, recursive=False)
            stream.flush()
            os.fsync(stream.fileno())
        with summed.open('x') as stream:
            stream.write(f'{sha256(packed)}  {archive.name}\n')
            stream.flush()
            os.fsync(stream.fileno())
        # Publish only complete bytes. Hard links fail rather than replacing a concurrent output.
        os.link(packed, archive)
        try:
            os.link(summed, checksum)
        except Exception:
            archive.unlink()
            raise
        sync_dir(output)
    print(archive)
    return archive


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--arch', choices=['amd64', 'arm64'], required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'artifacts/releases')
    parser.add_argument('--go', default=os.environ.get('CODEARTS_GO_BIN', 'go'))
    args = parser.parse_args()
    build(args.version, args.arch, args.output, args.go)
