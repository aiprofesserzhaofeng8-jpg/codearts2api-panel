#!/usr/bin/env python3
"""Build the Windows x64 ZIP with embedded UI; usable on Linux and Windows."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import zipfile
import sys
sys.path.insert(0, str(Path(__file__).resolve().parent))
from build_source import prepare_source

ROOT = Path(__file__).resolve().parents[1]


def sha256(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def build(version, output, go):
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-local\.[0-9]{8,14})?', version):
        raise ValueError('Expected a stable version such as v0.3.7')
    output.mkdir(parents=True, exist_ok=True)
    archive = output / f'codearts2api-panel-{version}-windows-amd64.zip'
    checksum = output / (archive.name + '.sha256')
    if any(path.exists() or path.is_symlink() for path in (archive, checksum)):
        raise FileExistsError('Refusing to replace an existing release')
    schema = int(re.search(r'const StateVersion = (\d+)',
                          (ROOT / 'backend/internal/storage/store.go').read_text(encoding='utf-8'))[1])
    build_source = prepare_source(ROOT, version)
    with tempfile.TemporaryDirectory(prefix='.windows-package-', dir=output) as temp:
        staging = Path(temp) / 'files'
        staging.mkdir()
        subprocess.run([go, 'build', '-trimpath', '-ldflags', f'-s -w -X main.version={version}',
                        '-o', str(staging / 'codearts2api-panel.exe'), './cmd/panel'],
                       cwd=ROOT / 'backend', check=True,
                       env={**os.environ, 'CGO_ENABLED': '0', 'GOOS': 'windows', 'GOARCH': 'amd64'})
        # An explicit allowlist keeps runtime state and credentials out of packages.
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
        for name in ('start.cmd', 'README.md'):
            text = (ROOT / 'deploy/windows' / name).read_text(encoding='utf-8')
            (staging / name).write_bytes(text.replace('\n', '\r\n').encode('utf-8'))
        files = {p.name: {'sha256': sha256(p), 'size': p.stat().st_size}
                 for p in sorted(staging.iterdir())}
        manifest = {'format': 1, 'kind': 'release', 'version': version, 'os': 'windows',
                    'arch': 'amd64', 'schema': schema, 'files': files, 'source': build_source}
        packed = Path(temp) / 'package.zip'
        with packed.open('xb') as stream:
            with zipfile.ZipFile(stream, 'w', compression=zipfile.ZIP_DEFLATED) as zip_file:
                zip_file.writestr('manifest.json', json.dumps(manifest, indent=2) + '\n')
                for name in files:
                    zip_file.write(staging / name, name)
            stream.flush()
            os.fsync(stream.fileno())
        summed = Path(temp) / 'package.sha256'
        with summed.open('x', encoding='ascii', newline='\n') as stream:
            stream.write(f'{sha256(packed)}  {archive.name}\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.link(packed, archive)
        try:
            os.link(summed, checksum)
        except Exception:
            archive.unlink()
            raise
    print(archive)
    return archive


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'artifacts/releases')
    parser.add_argument('--go', default=os.environ.get('CODEARTS_GO_BIN', 'go'))
    args = parser.parse_args()
    build(args.version, args.output, args.go)
