#!/usr/bin/env python3
"""Real root/systemd acceptance on an isolated CI host; no real provider accounts."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import sys
import tempfile
import types
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import post, wait


def run(first, second):
    assert os.geteuid() == 0 and Path('/run/systemd/system').is_dir()
    root = Path('/opt/codearts2api')
    unit = 'codearts2api.service'
    unit_file = Path('/etc/systemd/system') / unit
    assert not root.exists() and not root.is_symlink() and not unit_file.exists()
    assert ops.run(['systemctl', 'show', unit, '-p', 'LoadState', '--value']).stdout.strip() == 'not-found'
    try:
        pwd.getpwnam('codearts2api')
        raise AssertionError('requires an isolated host without an existing service account')
    except KeyError:
        pass
    installer = types.ModuleType('installer')
    source = Path(__file__).resolve().parents[1] / 'install.sh'
    exec(source.read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0], installer.__dict__)
    with tempfile.TemporaryDirectory(prefix='codearts-system-') as temp:
        folder = Path(temp)
        def execute(archive, argv):
            release = folder / secrets.token_hex(5)
            meta = ops.unpack(archive, release, 'release', ops.sha256(archive))
            tag = meta['version']
            blobs = {archive.name: archive.read_bytes(), 'panelctl.py': (release / 'panelctl.py').read_bytes()}
            blobs['SHA256SUMS'] = ''.join(hashlib.sha256(data).hexdigest() + '  ' + name + '\n' for name, data in blobs.items()).encode()
            metadata = {'tag_name': tag, 'assets': [{'name': n, 'browser_download_url': installer.BASE + tag + '/' + n} for n in blobs]}
            def fetch(url, target, limit):
                raw = json.dumps(metadata).encode() if url.startswith(installer.API) else blobs[url.rsplit('/', 1)[1]]
                assert len(raw) <= limit
                target.write_bytes(raw)
            with patch.object(installer, 'fetch', side_effect=fetch), patch.object(sys, 'argv', ['installer'] + argv):
                installer.main()
            return meta
        try:
            a = execute(first, [])  # Exact no-argument bootstrap defaults, including /opt and port 10087.
            base = 'http://127.0.0.1:10087'
            wait(base, 'setup_required')
            account = pwd.getpwnam('codearts2api')
            setup = root / 'data/state.json.setup.json'
            assert setup.stat().st_mode & 0o777 == 0o600 and setup.stat().st_uid == account.pw_uid
            password = secrets.token_urlsafe(24)
            post(base, '/setup/install', {'email': 'owner@example.com', 'password': password, 'confirm_password': password})
            wait(base, 'running')
            assert post(base, '/auth/login', {'username': 'owner@example.com', 'password': password})['user']['role'] == 'admin'
            binary = (root / 'current').resolve() / 'codearts2api-panel'
            assert binary.stat().st_uid == 0
            assert ops.run(['runuser', '-u', 'codearts2api', '--', 'test', '-w', str(binary)], check=False).returncode != 0
            assert ops.run(['runuser', '-u', 'codearts2api', '--', 'test', '-w', str(root / 'data')], check=False).returncode == 0
            pid = ops.run(['systemctl', 'show', unit, '-p', 'MainPID', '--value']).stdout.strip()
            uid_line = next(x for x in (Path('/proc') / pid / 'status').read_text().splitlines() if x.startswith('Uid:'))
            assert int(uid_line.split()[1]) == account.pw_uid != 0
            before = json.loads((root / 'data/state.json').read_text())
            # A second default invocation must fail without modifying the running installation.
            with patch.object(sys, 'argv', ['installer']):
                try:
                    installer.main()
                    raise AssertionError('duplicate installation accepted')
                except installer.InstallError:
                    pass
            ops.run(['systemctl', 'restart', unit]); wait(base, 'running')
            b = execute(second, ['upgrade'])  # Root/default-directory upgrade resolves the system scope.
            assert b['version'] != a['version']
            ops.verify_process(unit, (root / 'current').resolve() / 'codearts2api-panel', system=True)
            after = json.loads((root / 'data/state.json').read_text())
            for key in ('settings', 'keys', 'users', 'groups', 'stats', 'usage_sequence'):
                assert before.get(key) == after.get(key), key
            assert post(base, '/auth/login', {'username': 'owner@example.com', 'password': password})['user']['role'] == 'admin'
            assert len(list((root / 'backups').glob('*.tar.gz'))) == 1
            assert ops.run(['systemctl', 'is-enabled', unit]).stdout.strip() == 'enabled'
            print(json.dumps({'checks': ['no_argument_system_install', 'dedicated_nonroot_uid', 'root_owned_program', 'private_pending_marker', 'initial_setup_and_login', 'duplicate_rejected', 'restart', 'system_scope_upgrade', 'backup_created', 'password_and_state_preserved', 'boot_enabled'], 'network': 'release fixture; real systemd and program'}))
        finally:
            ops.run(['systemctl', 'disable', '--now', unit], check=False)
            unit_file.unlink(missing_ok=True)
            ops.run(['systemctl', 'daemon-reload'])
            if root.exists():
                shutil.rmtree(root)
            ops.run(['userdel', 'codearts2api'], check=False)
            ops.run(['groupdel', 'codearts2api'], check=False)
            (root.parent / '.codearts2api.panelops.lock').unlink(missing_ok=True)


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--first', type=Path, required=True)
    p.add_argument('--second', type=Path, required=True)
    args = p.parse_args()
    run(args.first.resolve(), args.second.resolve())
