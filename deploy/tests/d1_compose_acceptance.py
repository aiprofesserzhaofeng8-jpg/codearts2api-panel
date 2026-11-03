#!/usr/bin/env python3
"""D1 integration with a real Compose writer and a disposable host-readable bind mount."""
import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import port, post, wait


def run(image):
    project = 'd1-acceptance-' + secrets.token_hex(5)
    env = {k: v for k, v in os.environ.items() if not k.startswith('CODEARTS_')}
    env['COMPOSE_PROJECT_NAME'] = project
    with tempfile.TemporaryDirectory(prefix='d1-compose-') as temp:
        root = Path(temp)
        data = root / 'data'
        data.mkdir(mode=0o700)
        compose = root / 'compose.json'
        app_port = port()
        base = f'http://127.0.0.1:{app_port}'
        config = {'name': project, 'services': {'codearts2api': {
            'image': image, 'user': f'{os.getuid()}:{os.getgid()}', 'restart': 'unless-stopped',
            'ports': [f'127.0.0.1:{app_port}:10087'], 'volumes': [f'{data}:/data'],
            'command': ['-host', '0.0.0.0', '-port', '10087', '-data', '/data/state.json', '-no-import', '-no-refresh']}}}
        compose.write_text(json.dumps(config))
        prefix = ['docker', 'compose', '-p', project, '-f', str(compose)]
        def dc(*args, check=True):
            return subprocess.run(prefix + list(args), env=env, capture_output=True, text=True, check=check, timeout=60)
        def backup(archive, check=True):
            return subprocess.run([sys.executable, str(Path(ops.__file__)), 'backup', '--compose', str(compose),
                                   '--data-dir', str(data), '--archive', str(archive)], env=env,
                                  capture_output=True, text=True, check=check, timeout=60)
        checks = []
        try:
            dc('up', '-d', '--pull', 'never')
            wait(base, 'setup_required')
            email='owner@example.com'
            password = secrets.token_urlsafe(24)
            post(base, '/setup/install', {'email': email, 'password': password, 'confirm_password': password})
            wait(base, 'running')
            dc('pause', 'codearts2api')
            denied = root / 'paused.tar.gz'
            assert backup(denied, check=False).returncode != 0 and not denied.exists()
            dc('unpause', 'codearts2api')
            wait(base, 'running')
            checks.append('real_paused_compose_refused')
            invalid = data / 'invalid.tar.gz'
            assert backup(invalid, check=False).returncode != 0 and not invalid.exists()
            wait(base, 'running')
            checks.append('real_compose_failure_restarts_original_service')
            archive = root / 'managed.tar.gz'
            backup(archive)
            wait(base, 'running')
            checks.append('real_compose_backup_stops_and_restarts_writer')
            dc('stop', '-t', '30', 'codearts2api')
            offline = root / 'stopped.tar.gz'
            backup(offline)
            assert not dc('ps', '--status', 'running', '-q', 'codearts2api').stdout.strip()
            checks.append('originally_stopped_compose_stays_stopped')
            restored = root / 'restored'
            ops.unpack(offline, restored, 'backup')
            assert ops.inventory(data) == ops.inventory(restored)
            config['services']['codearts2api']['volumes'] = [f'{restored}:/data']
            compose.write_text(json.dumps(config))
            dc('up', '-d', '--pull', 'never')
            wait(base, 'running')
            assert post(base, '/auth/login', {'username': 'owner@example.com', 'password': password})['user']['role'] == 'admin'
            checks.append('restored_bind_mount_starts_with_original_login')
            print(json.dumps({'passed': True, 'checks': checks, 'real_provider_requests': 0}, indent=2))
        finally:
            dc('down', '-v', check=False)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', required=True)
    run(parser.parse_args().image)
