#!/usr/bin/env python3
"""Optional real user-systemd install/upgrade test; creates and removes its own unit."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import tarfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import port, post, wait


def failed_candidate(source, target):
    """Trusted synthetic release: reports its version, writes new data, never becomes ready."""
    with tarfile.open(source, 'r:gz') as archive:
        files = {member.name: archive.extractfile(member).read() for member in archive.getmembers()}
    meta = json.loads(files.pop('manifest.json'))
    major, minor, patch = ops.release_version(meta['version'])
    version = f'v{major}.{minor}.{patch + 1}'
    script = f'''#!{sys.executable}
import hashlib, json, os
from pathlib import Path
import sys, time
if '-version' in sys.argv:
    print('codearts2api {version}')
    sys.exit(0)
state = Path(sys.argv[sys.argv.index('-data') + 1])
snapshot = json.loads(state.read_text())
marker = state.parent / 'd3-synthetic-credential.json'
marker.write_text('synthetic-newest-credential-generation')
marker.chmod(0o600)
entry = {{'sequence': snapshot.get('usage_sequence', 0) + 1,
    'key_hash': hashlib.sha256(snapshot['keys'][0]['key'].encode()).hexdigest(),
    'entry': {{'request_id': 'd3-synthetic-settlement', 'time': int(time.time()), 'model': 'synthetic',
        'status': 200, 'attempts': 1, 'output_len': 2, 'usage': {{'known': True, 'input_tokens': 12, 'output_tokens': 4}},
        'estimated_cost': {{'currency': 'USD', 'amount': 0.125}}}}, 'out_chars': 2}}
journal = Path(str(state) + '.usage.jsonl')
with journal.open('a') as stream:
    journal.chmod(0o600)
    stream.write(json.dumps(entry) + '\\n')
    stream.flush()
    os.fsync(stream.fileno())
while True:
    time.sleep(1)
'''.encode()
    files['data/codearts2api-panel'] = script
    meta['version'] = version
    meta['files']['codearts2api-panel'] = {'size': len(script), 'sha256': hashlib.sha256(script).hexdigest()}
    files['manifest.json'] = json.dumps(meta).encode()
    with tarfile.open(target, 'w:gz') as archive:
        for name, body in files.items():
            member = tarfile.TarInfo(name)
            member.size = len(body)
            archive.addfile(member, io.BytesIO(body))
    return version


def run(first, second, failure_cases=False):
    name = 'acceptance-' + secrets.token_hex(5)
    unit = 'codearts2api-' + name + '.service'
    unit_path = Path(os.environ.get('XDG_CONFIG_HOME', str(Path.home() / '.config'))) / 'systemd/user' / unit
    with tempfile.TemporaryDirectory(prefix='codearts-systemd-') as temporary:
        root = Path(temporary) / 'install'
        app_port = port()
        base = f'http://127.0.0.1:{app_port}'
        controller = Path(temporary) / 'controller'
        ops.unpack(second, controller, 'release', ops.sha256(second))
        ctl = [sys.executable, str(controller / 'panelctl.py')]
        try:
            subprocess.run(ctl + ['install', '--root', str(root), '--name', name, '--port', str(app_port),
                                 '--archive', str(first), '--sha256', ops.sha256(first)], check=True)
            wait(base, 'setup_required')
            token = json.loads((root / 'data/state.json.setup.json').read_text())['setup_token']
            password = secrets.token_urlsafe(24)
            post(base, '/setup/install', {'setup_token': token, 'password': password, 'confirm_password': password})
            wait(base, 'running')
            live_archive = Path(temporary) / 'should-not-exist.tar.gz'
            denied = subprocess.run(ctl + ['backup', '--offline', '--data-dir', str(root / 'data'),
                                           '--archive', str(live_archive)], capture_output=True)
            assert denied.returncode != 0 and not live_archive.exists()
            invalid = root / 'data/invalid-backup.tar.gz'
            failed = subprocess.run(ctl + ['backup', '--unit', unit, '--data-dir', str(root / 'data'),
                                           '--archive', str(invalid)], capture_output=True)
            assert failed.returncode != 0 and not invalid.exists()
            wait(base, 'running')
            old = (root / 'current').resolve()
            subprocess.run(ctl + ['upgrade', '--root', str(root), '--archive', str(second),
                                 '--sha256', ops.sha256(second)], check=True)
            assert (root / 'current').resolve() != old
            assert old.is_dir()
            assert post(base, '/auth/login', {'username': 'admin', 'password': password})['user']['role'] == 'admin'
            archives = list((root / 'backups').glob('*.tar.gz'))
            assert len(archives) == 1
            ops.unpack(archives[0], Path(temporary) / 'backup-verification', 'backup')
            # Exercise the public CLI's managed stop/backup/restart path and locks.
            subprocess.run(ctl + ['backup', '--unit', unit,
                            '--data-dir', str(root / 'data'), '--archive', str(Path(temporary) / 'managed.tar.gz')], check=True)
            wait(base, 'running')
            checks = ['real_systemd_install', 'real_systemd_upgrade_and_executable_verification',
                                                        'private_backup_validated', 'login_survives_upgrade', 'managed_backup_restarts_service',
                                                        'real_live_writer_refused_by_offline_cli', 'real_backup_failure_restarts_service']
            if failure_cases:
                pid = ops.run(['systemctl', '--user', 'show', unit, '-p', 'MainPID', '--value']).stdout.strip()
                for archive, digest in ((first, ops.sha256(first)), (second, '0' * 64)):
                    rejected = ops.parser().parse_args(['upgrade', '--root', str(root), '--archive', str(archive), '--sha256', digest])
                    try:
                        ops.upgrade(rejected)
                        raise AssertionError('unsafe upgrade was accepted')
                    except ops.OpsError:
                        pass
                    assert ops.run(['systemctl', '--user', 'show', unit, '-p', 'MainPID', '--value']).stdout.strip() == pid
                checks.append('real_downgrade_and_bad_digest_refused_without_restart')
                failure = Path(temporary) / 'failed-candidate.tar.gz'
                version = failed_candidate(second, failure)
                candidate_args = ops.parser().parse_args(['upgrade', '--root', str(root), '--archive', str(failure), '--sha256', ops.sha256(failure)])
                try:
                    ops.upgrade(candidate_args)
                    raise AssertionError('unhealthy candidate was accepted')
                except ops.OpsError:
                    pass
                assert (root / 'current').resolve().name == version
                assert not ops.Service(unit=unit).running()
                assert old.is_dir()
                assert (root / 'data/d3-synthetic-credential.json').read_text() == 'synthetic-newest-credential-generation'
                journal = (root / 'data/state.json.usage.jsonl').read_text()
                assert 'd3-synthetic-settlement' in journal
                assert len(list((root / 'backups').glob('*.tar.gz'))) == 2
                checks.append('real_unhealthy_upgrade_stops_candidate_and_preserves_newest_data_and_journal')
            print(json.dumps({'passed': True, 'checks': checks,
                              'existing_services_changed': False}, indent=2))
        finally:
            subprocess.run(['systemctl', '--user', 'disable', '--now', unit], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            unit_path.unlink(missing_ok=True)
            subprocess.run(['systemctl', '--user', 'daemon-reload'], check=True)
            subprocess.run(['systemctl', '--user', 'reset-failed', unit], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--first', required=True, type=Path)
    parser.add_argument('--second', required=True, type=Path)
    parser.add_argument('--failure-cases', action='store_true', help='Also test downgrade/digest refusal and an unready synthetic release')
    args = parser.parse_args()
    run(args.first.resolve(), args.second.resolve(), args.failure_cases)
