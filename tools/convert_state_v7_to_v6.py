#!/usr/bin/env python3
"""Offline, lossless compatibility export for M5; never overwrite the input.

Stop the writer first. Copy its latest entire data directory, then run this on
that copy. Refuses defaults/list/admission rules M5 cannot represent. Prompt
inheritance is flattened into explicit platform settings, preserving behavior.
The journal sidecar is copied under the output filename as well.
"""
import argparse
import copy
import json
import os
from pathlib import Path


def convert(state):
    if state.get('version') != 7:
        raise ValueError('Expected schema 7')
    result = copy.deepcopy(state)
    for group in result.get('groups', []):
        if group.get('default_model') or group.get('client_default_model') or group.get('model_rules'):
            raise ValueError('M5 cannot represent group defaults or model rules; clear them in M6 first')
        for key in ('default_model', 'client_default_model', 'model_rules'):
            group.pop(key, None)
    settings = result['settings']
    common = settings.pop('common_prompt')
    for name in ('codearts', 'workbuddy', 'trae'):
        platform = settings[name]
        overrides = platform.pop('prompt_overrides', None)
        if overrides is not None:
            for key in ('prompt_mode', 'prompt_text', 'prompt_fallback', 'sanitize'):
                platform[key] = overrides[key] if overrides.get(key) is not None else common[key]
    result['version'] = 6
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('input', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    if args.input.resolve() == args.output.resolve():
        raise ValueError('Output must be a new path')
    result = convert(json.loads(args.input.read_text()))
    journal = Path(str(args.input) + '.usage.jsonl')
    target = Path(str(args.output) + '.usage.jsonl')
    if args.output.exists() or target.exists():
        raise ValueError('Output or sidecar already exists')
    # Exclusive private writes: failures leave input and its journal untouched.
    os.umask(0o077)
    with args.output.open('x') as stream:
        json.dump(result, stream, ensure_ascii=False, indent=2)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    if journal.exists():
        with target.open('xb') as stream:
            stream.write(journal.read_bytes())
            stream.flush()
            os.fsync(stream.fileno())
    print('Exported schema 6 and current journal; source untouched. Verify with the M5 binary before switching.')

if __name__ == '__main__':
    main()
