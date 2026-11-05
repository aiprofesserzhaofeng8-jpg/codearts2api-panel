#!/usr/bin/env python3
"""Exercise the built U2 migration CLI with synthetic, isolated state."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--old-binary', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    checks = []

    def invoke(*flags, expected=0):
        result = subprocess.run([str(binary), *map(str, flags)], capture_output=True, text=True, timeout=30)
        assert result.returncode == expected, f'CLI returned {result.returncode}; expected {expected}'
        return result

    with tempfile.TemporaryDirectory(prefix='codearts-u2-') as tmp:
        root = Path(tmp)
        source = root / 'source'
        source.mkdir(mode=0o700)
        state = source / 'state.json'
        value = {'version': 10, 'settings': {}, 'accounts': [], 'keys': [],
                 'groups': [{'id': 'fixture-group', 'name': 'fixture', 'account_ids': [],
                             'model_mappings': [{'name': 'deepseek-4.1-flash', 'sources':
                                 ['workbuddy/cn/DeepSeek-4.1', 'trae/cn/DeepSeek-4.1']}]}]}
        state.write_text(json.dumps(value))
        state.chmod(0o600)
        (source / 'state.json.growth.json').write_text('synthetic-sidecar')
        digest = hashlib.sha256(state.read_bytes()).hexdigest()
        report = json.loads(invoke('-check-state-migration', state).stdout)
        assert report['ready'] and report['definitions'] == 1
        assert hashlib.sha256(state.read_bytes()).hexdigest() == digest
        checks.append('precheck_read_only')
        target = root / 'converted'
        invoke('-migrate-state-dir', source, '-migration-output-dir', target)
        converted = json.loads((target / 'state.json').read_text())
        assert converted['version'] == 11 and len(converted['unified_models']) == 1
        assert converted['groups'][0]['unified_models'][0]['source_mode'] == 'selected'
        assert 'model_mappings' not in converted['groups'][0]
        assert (target / 'state.json.growth.json').read_text() == 'synthetic-sidecar'
        assert hashlib.sha256(state.read_bytes()).hexdigest() == digest
        assert (target / 'state.json').stat().st_mode & 0o777 == 0o600
        checks.append('offline_copy_permissions_and_schema')
        again = json.loads(invoke('-check-state-migration', target / 'state.json').stdout)
        assert again['ready'] and again['from_schema'] == 11
        checks.append('schema11_precheck_idempotent')
        invoke('-migrate-state-dir', source, '-migration-output-dir', target, expected=1)
        checks.append('existing_output_rejected')
        conflict = json.loads(json.dumps(value))
        group = json.loads(json.dumps(conflict['groups'][0]))
        group['id'] = 'other-group'
        group['model_mappings'][0]['sources'] = ['kiro/us-east-1/DeepSeek-4.1']
        conflict['groups'].append(group)
        bad = root / 'conflict.json'
        bad.write_text(json.dumps(conflict))
        result = json.loads(invoke('-check-state-migration', bad, expected=2).stdout)
        assert any(x['reason'] == 'name_sources_conflict' for x in result['conflicts'])
        checks.append('conflict_exit_code_2')
        # The guard detects another live process explicitly owning this path.
        writer = subprocess.Popen(['python3', '-c', 'import time; time.sleep(30)', '-data', str(state)])
        try:
            invoke('-migrate-state-dir', source, '-migration-output-dir', root / 'busy', expected=1)
            assert not (root / 'busy').exists()
        finally:
            writer.terminate()
            writer.wait(timeout=5)
        checks.append('active_writer_rejected')
        if args.old_binary:
            result = subprocess.run([str(args.old_binary.resolve()), '-data', str(target / 'state.json'), '-no-import'],
                                    capture_output=True, text=True, timeout=10)
            assert result.returncode != 0 and '11' in result.stderr and '10' in result.stderr
            checks.append('old_binary_rejects_schema11')
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps({'status': 'passed', 'checks': checks,
        'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
        'scope': 'Synthetic state only; no production data or upstream requests.'}, indent=2) + '\n')
    print(f'{len(checks)} isolated CLI checks passed')


if __name__ == '__main__':
    main()
