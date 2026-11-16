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

from build_source import prepare_source
from panelctl import inventory, release_version, require, sha256, sync_dir

ROOT = Path(__file__).resolve().parents[1]


def build(version, arch, output, go):
    release_version(version)
    require(arch in ('amd64', 'arm64'), '不支持的发布架构')
    schema_source = (ROOT / 'backend/internal/storage/store.go').read_text()
    schema = int(re.search(r'const StateVersion = (\d+)', schema_source)[1])
    output.mkdir(parents=True, exist_ok=True)
    archive = output / f'codearts2api-panel-{version}-linux-{arch}.tar.gz'
    checksum = output / (archive.name + '.sha256')
    if any(p.exists() or p.is_symlink() for p in (archive, checksum)):
        raise RuntimeError('发布包已存在，拒绝覆盖')
    build_source = prepare_source(ROOT, version)
    with tempfile.TemporaryDirectory(prefix='.package-', dir=output) as temp:
        staging = Path(temp) / 'data'
        staging.mkdir()
        subprocess.run([go, 'build', '-trimpath', '-ldflags', f'-s -w -X main.version={version}',
                        '-o', str(staging / 'codearts2api-panel'), './cmd/panel'], cwd=ROOT / 'backend',
                       env={**os.environ, 'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': arch}, check=True)
        for name in ('panelctl.py', 'install.sh', 'healthcheck.sh', 'Caddyfile', 'compose.production.yml', '.env.example', 'README.md', 'UNIFIED-MODELS.md'):
            (staging / name).write_bytes((ROOT / 'deploy' / name).read_bytes())
        for name in ('LICENSE', 'NOTICE', 'LICENSE.workbuddy', 'LICENSE.qoder', 'LICENSE.kiro', 'LICENSE.teleagent'):
            (staging / name).write_bytes((ROOT / name).read_bytes())
        for name, source in (
            ('LICENSE.sub2api', 'backend/internal/catalog/modelmeta/LICENSE'),
            ('COPYING.sub2api', 'backend/internal/catalog/modelmeta/COPYING'),
            ('SUB2API-METADATA.md', 'backend/internal/catalog/modelmeta/UPSTREAM.md'),
            ('SUB2API-METADATA.json', 'backend/internal/catalog/modelmeta/upstream.json'),
            ('SUB2API-PROTOCOL.md', 'backend/internal/responsescompat/UPSTREAM.md'),
            ('SUB2API-CNAPI.md', 'backend/internal/cnapi/UPSTREAM.md'),
            ('SUB2API-CNAPI.json', 'backend/internal/cnapi/upstream.json'),
            ('LICENSE.cnapi', 'backend/internal/cnapi/LICENSE'),
            ('COPYING.cnapi', 'backend/internal/cnapi/COPYING'),
            ('SUB2API-GEMINI.md', 'backend/internal/gemini/UPSTREAM.md'),
            ('SUB2API-GEMINI.json', 'backend/internal/gemini/upstream.json'),
            ('LICENSE.gemini', 'backend/internal/gemini/LICENSE'),
            ('COPYING.gemini', 'backend/internal/gemini/COPYING'),
            ('SUB2API-ANTIGRAVITY.md', 'backend/internal/antigravity/UPSTREAM.md'),
            ('SUB2API-ANTIGRAVITY.json', 'backend/internal/antigravity/upstream.json'),
            ('LICENSE.antigravity', 'backend/internal/antigravity/LICENSE'),
            ('COPYING.antigravity', 'backend/internal/antigravity/COPYING'),
            ('SUB2API-GROK.md', 'backend/internal/grok/UPSTREAM.md'),
            ('SUB2API-GROK.json', 'backend/internal/grok/upstream.json'),
            ('LICENSE.grok', 'backend/internal/grok/LICENSE'),
            ('COPYING.grok', 'backend/internal/grok/COPYING'),
            ('SUB2API-OPENCODE.md', 'backend/internal/opencode/UPSTREAM.md'),
            ('SUB2API-OPENCODE.json', 'backend/internal/opencode/upstream.json'),
            ('LICENSE.opencode', 'backend/internal/opencode/LICENSE'),
            ('COPYING.opencode', 'backend/internal/opencode/COPYING'),
            ('SUB2API-COMMANDCODE.md', 'backend/internal/commandcode/UPSTREAM.md'),
            ('SUB2API-COMMANDCODE.json', 'backend/internal/commandcode/upstream.json'),
            ('LICENSE.commandcode', 'backend/internal/commandcode/LICENSE'),
            ('COPYING.commandcode', 'backend/internal/commandcode/COPYING'),
            ('SUB2API-CLINE.md', 'backend/internal/cline/UPSTREAM.md'),
            ('SUB2API-CLINE.json', 'backend/internal/cline/upstream.json'),
            ('LICENSE.cline', 'backend/internal/cline/LICENSE'),
            ('COPYING.cline', 'backend/internal/cline/COPYING'),
            ('SUB2API-VERTEX.md', 'backend/internal/vertex/UPSTREAM.md'),
            ('SUB2API-VERTEX.json', 'backend/internal/vertex/upstream.json'),
            ('LICENSE.vertex', 'backend/internal/vertex/LICENSE'),
            ('COPYING.vertex', 'backend/internal/vertex/COPYING'),
            ('SUB2API-BEDROCK.md', 'backend/internal/bedrock/UPSTREAM.md'),
            ('SUB2API-BEDROCK.json', 'backend/internal/bedrock/upstream.json'),
            ('LICENSE.bedrock', 'backend/internal/bedrock/LICENSE'),
            ('COPYING.bedrock', 'backend/internal/bedrock/COPYING'),
            ('LICENSE.aws-sdk-go-v2', 'backend/internal/bedrock/LICENSE.aws-sdk-go-v2'),
            ('NOTICE.aws-sdk-go-v2', 'backend/internal/bedrock/NOTICE.aws-sdk-go-v2'),
            ('LICENSE.smithy-go', 'backend/internal/bedrock/LICENSE.smithy-go'),
            ('NOTICE.smithy-go', 'backend/internal/bedrock/NOTICE.smithy-go'),
            ('SUB2API-TYPESAFE.md', 'backend/internal/typesafe/UPSTREAM.md'),
            ('SUB2API-TYPESAFE.json', 'backend/internal/typesafe/upstream.json'),
            ('LICENSE.typesafe', 'backend/internal/typesafe/LICENSE'),
            ('COPYING.typesafe', 'backend/internal/typesafe/COPYING'),
            ('SUB2API-GOOGLEWIRE.md', 'backend/internal/googlewire/UPSTREAM.md'),
            ('SUB2API-GOOGLEWIRE.json', 'backend/internal/googlewire/upstream.json'),
            ('LICENSE.googlewire', 'backend/internal/googlewire/LICENSE'),
            ('COPYING.googlewire', 'backend/internal/googlewire/COPYING'),
        ):
            (staging / name).write_bytes((ROOT / source).read_bytes())
        files = inventory(staging)
        meta = {'format': 1, 'kind': 'release', 'version': version, 'os': 'linux', 'arch': arch,
                'schema': schema, 'files': files, 'source': build_source}
        if schema == 11:
            meta['state_migrations'] = [{'from': 10, 'to': 11}]
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
