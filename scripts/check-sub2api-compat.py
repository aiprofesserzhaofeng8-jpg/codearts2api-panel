#!/usr/bin/env python3
"""Run the pinned upstream bridge tests without adding production dependencies."""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import urllib.request

root = Path(__file__).resolve().parents[1]
source = root / 'backend/internal/responsescompat'
manifest = json.loads((source / 'upstream.json').read_text())
go = os.environ.get('CODEARTS_GO_BIN', 'go')
cache = os.environ.get('SUB2API_REFERENCE_DIR')

def test_source(name, expected):
    cached = Path(cache) / 'backend/internal/pkg/apicompat' / name if cache else None
    if cached and cached.exists():
        data = cached.read_bytes()
    else:
        url = ('https://raw.githubusercontent.com/Wei-Shaw/sub2api/' + manifest['revision']
               + '/backend/internal/pkg/apicompat/' + name)
        data = urllib.request.urlopen(url, timeout=30).read()
    if hashlib.sha256(data).hexdigest() != expected:
        raise RuntimeError('Upstream source/test hash mismatch: ' + name)
    return name, data

def verify_sources(source, manifest):
    originals = manifest['source_files']
    adapted = manifest.get('adapted_source_files', {})
    if set(adapted) - set(originals):
        raise RuntimeError('Adapted source is not in the pinned upstream manifest')
    for name, original in originals.items():
        entry = adapted.get(name)
        if entry is not None and (not entry.get('reason') or not entry.get('sha256')):
            raise RuntimeError('Adapted source must record a hash and reason: ' + name)
        expected = entry['sha256'] if entry is not None else original
        if hashlib.sha256((source / name).read_bytes()).hexdigest() != expected:
            raise RuntimeError('Unreviewed compatibility source change: ' + name)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--local-only", action="store_true", help="Check reviewed local source hashes without network access")
    args = parser.parse_args()
    verify_sources(source, manifest)
    if args.local_only:
        print("Reviewed compatibility source hashes passed")
        return
    # Verify the upstream originals independently; local hashes must not replace
    # provenance hashes merely to make a modified file pass.
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        for future in [executor.submit(test_source, n, h) for n, h in manifest['source_files'].items()]:
            future.result()
    with tempfile.TemporaryDirectory(prefix='codearts-sub2api-tests-') as directory:
        target = Path(directory)
        for file in source.glob('*.go'):
            shutil.copy2(file, target / file.name)
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
            futures = [executor.submit(test_source, n, h) for n, h in manifest['tests'].items()]
            for future in futures:
                name, data = future.result()
                (target / ('upstream_' + name)).write_bytes(data)
        (target / 'go.mod').write_text('module upstreamcompatcheck\n\ngo 1.24\n\nrequire github.com/stretchr/testify v1.11.1\n')
        subprocess.run([go, 'mod', 'tidy'], cwd=target, check=True)
        subprocess.run([go, 'test', '-race', '-count=1', '-v', './...'], cwd=target, check=True)
    print('Pinned upstream provenance, reviewed local sources and regression tests passed:', manifest['revision'])


if __name__ == '__main__':
    main()
