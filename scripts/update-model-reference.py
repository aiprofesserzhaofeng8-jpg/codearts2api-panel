#!/usr/bin/env python3
"""Refresh the vendored models.dev reference; does not change runtime policies or Pi."""
import argparse
import datetime
import json
from pathlib import Path
import re
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / 'backend/internal/catalog/modelmeta/data/models.dev.json'
URL = 'https://models.dev/api.json'
LIMIT = 8 << 20


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, help='Use an already downloaded models.dev JSON')
    parser.add_argument('--source-revision', help='models.dev Git revision used to generate --source')
    args = parser.parse_args()
    if args.source:
        body = args.source.read_bytes()
    else:
        with urllib.request.urlopen(URL, timeout=30) as response:
            body = response.read(LIMIT + 1)
    if len(body) > LIMIT:
        raise ValueError('Model registry exceeds 8 MiB; existing snapshot preserved')
    registry = json.loads(body)
    source = (ROOT / 'backend/internal/catalog/client_references.go').read_text()
    providers = re.findall(r'"([^"]+)"', re.search(r'var referenceProviders = \[\]string\{([^}]+)\}', source)[1])
    selected = {}
    for name in providers:
        provider = registry.get(name, {})
        models = {}
        for key, model in provider.get('models', {}).items():
            limits = model.get('limit', {})
            numeric = {field: limits[field] for field in ('context', 'output')
                       if type(limits.get(field)) is int and 0 < limits[field] <= 2097152}
            if numeric:
                models[key] = {'id': model.get('id', key), 'name': model.get('name', key), 'limit': numeric}
        if models:
            selected[name] = {'id': name, 'models': models}
    if not selected:
        raise ValueError('No usable model limits; existing snapshot preserved')
    value = json.dumps(selected, ensure_ascii=False, sort_keys=True, indent=2) + '\n'
    temporary = DEST.with_suffix('.tmp')
    temporary.write_text(value)
    temporary.replace(DEST)
    count = sum(len(p['models']) for p in selected.values())
    (DEST.parent / 'snapshot.json').write_text(json.dumps({
        'source': URL, 'retrieved_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'providers': list(selected), 'model_entries': count,
        'fields': ['id', 'name', 'limit.context', 'limit.output'],
        'license': 'MIT', 'source_revision': args.source_revision,
        'acquisition': 'local generated API JSON' if args.source else 'public API',
    }, ensure_ascii=False, indent=2) + '\n')
    print(f'Updated {count} reference entries across {len(selected)} providers: {DEST}')


if __name__ == '__main__':
    main()
