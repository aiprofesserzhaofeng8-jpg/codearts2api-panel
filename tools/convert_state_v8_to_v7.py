#!/usr/bin/env python3
"""Offline M6 compatibility export. Stop the writer; use the latest whole data.

M6 cannot enforce user ownership or aggregate budgets. Refuse any user identity,
owned key or user-attributed usage in the snapshot AND pending journal. Never
overwrite input/output or restore an older credential backup.
"""
import argparse
import copy
import json
import os
from pathlib import Path


def convert(state, journal=b''):
    if state.get('version') != 8:
        raise ValueError('Expected schema 8')
    if state.get('users'):
        raise ValueError('M6 cannot represent user identities or aggregate budgets')
    for key in state.get('keys', []):
        if key.get('user_id'):
            raise ValueError('M6 cannot enforce key ownership')
    for entries in (state.get('logs', []), state.get('usage_records', [])):
        if any(entry.get('user_id') for entry in entries):
            raise ValueError('M6 cannot preserve user usage ownership')
    for line in journal.splitlines():
        if not line:
            continue
        record = json.loads(line)
        if record.get('entry', {}).get('user_id'):
            raise ValueError('Pending journal contains user-attributed consumption')
    result = copy.deepcopy(state)
    result.pop('users', None)
    for key in result.get('keys', []):
        key.pop('user_id', None)
    for entries in (result.get('logs', []), result.get('usage_records', [])):
        for entry in entries:
            entry.pop('user_id', None)
    result['version'] = 7
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('input', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    journal = Path(str(args.input) + '.usage.jsonl')
    target = Path(str(args.output) + '.usage.jsonl')
    if args.input.resolve() == args.output.resolve() or args.output.exists() or target.exists():
        raise ValueError('Output and journal must be new paths')
    raw = journal.read_bytes() if journal.exists() else b''
    result = convert(json.loads(args.input.read_text()), raw)
    os.umask(0o077)
    with args.output.open('x') as stream:
        json.dump(result, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    if journal.exists():
        with target.open('xb') as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
    print('Exported schema 7 and latest journal; source untouched. Verify with M6 before switching.')


if __name__ == '__main__':
    main()
