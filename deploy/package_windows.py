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
        if hasattr(hashlib, 'file_digest'):
            return hashlib.file_digest(stream, 'sha256').hexdigest()
        h = hashlib.sha256()
        while chunk := stream.read(65536):
            h.update(chunk)
        return h.hexdigest()


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
            ('SUB2API-GEMINI.md', 'backend/internal/upstream/gemini/UPSTREAM.md'),
            ('SUB2API-GEMINI.json', 'backend/internal/upstream/gemini/upstream.json'),
            ('LICENSE.gemini', 'backend/internal/upstream/gemini/LICENSE'),
            ('COPYING.gemini', 'backend/internal/upstream/gemini/COPYING'),
            ('SUB2API-ANTIGRAVITY.md', 'backend/internal/upstream/antigravity/UPSTREAM.md'),
            ('SUB2API-ANTIGRAVITY.json', 'backend/internal/upstream/antigravity/upstream.json'),
            ('LICENSE.antigravity', 'backend/internal/upstream/antigravity/LICENSE'),
            ('COPYING.antigravity', 'backend/internal/upstream/antigravity/COPYING'),
            ('SUB2API-GROK.md', 'backend/internal/upstream/grok/UPSTREAM.md'),
            ('SUB2API-GROK.json', 'backend/internal/upstream/grok/upstream.json'),
            ('LICENSE.grok', 'backend/internal/upstream/grok/LICENSE'),
            ('COPYING.grok', 'backend/internal/upstream/grok/COPYING'),
            ('SUB2API-OPENCODE.md', 'backend/internal/upstream/opencode/UPSTREAM.md'),
            ('SUB2API-OPENCODE.json', 'backend/internal/upstream/opencode/upstream.json'),
            ('LICENSE.opencode', 'backend/internal/upstream/opencode/LICENSE'),
            ('COPYING.opencode', 'backend/internal/upstream/opencode/COPYING'),
            ('SUB2API-COMMANDCODE.md', 'backend/internal/upstream/commandcode/UPSTREAM.md'),
            ('SUB2API-COMMANDCODE.json', 'backend/internal/upstream/commandcode/upstream.json'),
            ('LICENSE.commandcode', 'backend/internal/upstream/commandcode/LICENSE'),
            ('COPYING.commandcode', 'backend/internal/upstream/commandcode/COPYING'),
            ('SUB2API-CLINE.md', 'backend/internal/upstream/cline/UPSTREAM.md'),
            ('SUB2API-CLINE.json', 'backend/internal/upstream/cline/upstream.json'),
            ('LICENSE.cline', 'backend/internal/upstream/cline/LICENSE'),
            ('COPYING.cline', 'backend/internal/upstream/cline/COPYING'),
            ('SUB2API-VERTEX.md', 'backend/internal/upstream/vertex/UPSTREAM.md'),
            ('SUB2API-VERTEX.json', 'backend/internal/upstream/vertex/upstream.json'),
            ('LICENSE.vertex', 'backend/internal/upstream/vertex/LICENSE'),
            ('COPYING.vertex', 'backend/internal/upstream/vertex/COPYING'),
            ('SUB2API-BEDROCK.md', 'backend/internal/upstream/bedrock/UPSTREAM.md'),
            ('SUB2API-BEDROCK.json', 'backend/internal/upstream/bedrock/upstream.json'),
            ('LICENSE.bedrock', 'backend/internal/upstream/bedrock/LICENSE'),
            ('COPYING.bedrock', 'backend/internal/upstream/bedrock/COPYING'),
            ('LICENSE.aws-sdk-go-v2', 'backend/internal/upstream/bedrock/LICENSE.aws-sdk-go-v2'),
            ('NOTICE.aws-sdk-go-v2', 'backend/internal/upstream/bedrock/NOTICE.aws-sdk-go-v2'),
            ('LICENSE.smithy-go', 'backend/internal/upstream/bedrock/LICENSE.smithy-go'),
            ('NOTICE.smithy-go', 'backend/internal/upstream/bedrock/NOTICE.smithy-go'),
            ('SUB2API-TYPESAFE.md', 'backend/internal/upstream/typesafe/UPSTREAM.md'),
            ('SUB2API-TYPESAFE.json', 'backend/internal/upstream/typesafe/upstream.json'),
            ('LICENSE.typesafe', 'backend/internal/upstream/typesafe/LICENSE'),
            ('COPYING.typesafe', 'backend/internal/upstream/typesafe/COPYING'),
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
