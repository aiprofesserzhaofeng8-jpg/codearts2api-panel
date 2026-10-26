#!/usr/bin/env python3
"""Run the pinned upstream bridge tests without adding production dependencies."""
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
source = root / 'internal/responsescompat'
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
        raise RuntimeError('Upstream test hash mismatch: ' + name)
    return name, data

for name, expected in manifest['source_files'].items():
    if hashlib.sha256((source / name).read_bytes()).hexdigest() != expected:
        raise RuntimeError('Vendored source differs from pinned Sub2API: ' + name)
with tempfile.TemporaryDirectory(prefix='codearts-sub2api-tests-') as directory:
    target = Path(directory)
    for file in source.glob('*.go'):
        shutil.copy2(file, target / file.name)
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        futures = [executor.submit(test_source, n, h) for n, h in manifest['tests'].items()]
        for future in futures:
            name, data = future.result()
            (target / name).write_bytes(data)
    (target / 'go.mod').write_text('module upstreamcompatcheck\n\ngo 1.24\n\nrequire github.com/stretchr/testify v1.11.1\n')
    subprocess.run([go, 'mod', 'tidy'], cwd=target, check=True)
    subprocess.run([go, 'test', '-race', '-count=1', '-v', './...'], cwd=target, check=True)
print('Pinned Sub2API conversion sources and upstream regression tests passed:', manifest['revision'])
