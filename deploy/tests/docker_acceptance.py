#!/usr/bin/env python3
"""Production Compose acceptance using only a disposable project and volume."""
import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import port, post, wait


def run(image):
    project = 'panel-acceptance-' + secrets.token_hex(5)
    compose = Path(ops.__file__).parent / 'compose.production.yml'
    app_port = port()
    base = f'http://127.0.0.1:{app_port}'
    env = {**os.environ, 'CODEARTS_IMAGE': image, 'CODEARTS_PORT': str(app_port)}
    prefix = ['docker', 'compose', '-p', project, '-f', str(compose)]
    def dc(*args, check=True):
        result = subprocess.run(prefix + list(args), env=env, capture_output=True, text=True, timeout=90)
        if check and result.returncode:
            raise RuntimeError('isolated Compose command failed: ' + args[0])
        return result
    with tempfile.TemporaryDirectory(prefix='codearts-docker-') as temp:
        root = Path(temp)
        try:
            dc('up', '-d', '--pull', 'never')
            wait(base, 'setup_required')
            assert dc('exec', '-T', 'codearts2api', 'sh', '/usr/local/bin/codearts-healthcheck').stdout.strip() == 'setup_required'
            assert dc('exec', '-T', 'codearts2api', 'sh', '/usr/local/bin/codearts-healthcheck', '--ready', check=False).returncode != 0
            token = json.loads(dc('exec', '-T', 'codearts2api', 'cat', '/data/codearts2api-data.json.setup.json').stdout)['setup_token']
            password = secrets.token_urlsafe(24)
            post(base, '/setup/install', {'setup_token': token, 'password': password, 'confirm_password': password})
            wait(base, 'running')
            assert dc('exec', '-T', 'codearts2api', 'sh', '/usr/local/bin/codearts-healthcheck', '--ready').stdout.strip() == 'running'
            assert dc('exec', '-T', 'codearts2api', 'id', '-u').stdout.strip() == '10001'
            cid = dc('ps', '-q', 'codearts2api').stdout.strip()
            container = json.loads(subprocess.check_output(['docker', 'inspect', cid], text=True))[0]
            bindings = container['NetworkSettings']['Ports']['10087/tcp']
            assert bindings and all(binding['HostIp'] == '127.0.0.1' for binding in bindings)
            assert 'ALL' in container['HostConfig']['CapDrop']
            assert 'no-new-privileges:true' in container['HostConfig']['SecurityOpt']
            failed = dc('exec', '-T', '-e', 'CODEARTS_HEALTH_URL=http://127.0.0.1:1',
                'codearts2api', 'sh', '/usr/local/bin/codearts-healthcheck', check=False)
            assert failed.returncode != 0 and failed.stdout.strip() == 'unhealthy'
            for _ in range(40):
                inspect = subprocess.run(['docker', 'inspect', '--format', '{{.State.Health.Status}}', cid], capture_output=True, text=True, check=True)
                if inspect.stdout.strip() == 'healthy':
                    break
                time.sleep(1)
            assert inspect.stdout.strip() == 'healthy'
            dc('stop', '-t', '30', 'codearts2api')
            staging = root / 'staging'
            staging.mkdir(mode=0o700)
            dc('cp', 'codearts2api:/data/.', str(staging) + '/')
            dc('start', 'codearts2api')
            wait(base, 'running')
            archive = root / 'volume.tar.gz'
            with ops.stopped(staging, offline=True):
                ops.backup(staging, archive, 'codearts2api-data.json')
            restored = root / 'restored'
            ops.unpack(archive, restored, 'backup')
            assert ops.inventory(restored) == ops.inventory(staging)
            assert post(base, '/auth/login', {'username': 'admin', 'password': password})['user']['role'] == 'admin'
            print(json.dumps({'passed': True, 'checks': ['production_compose_loopback_nonroot', 'container_setup_and_readiness',
                                                       'container_running_health', 'container_unreachable_health_fails',
                                                       'stopped_volume_copy_backup_restore', 'login_survives_restart'],
                              'existing_containers_changed': False}, indent=2))
        finally:
            # -v is intentionally restricted to this unique disposable test project.
            dc('down', '-v', check=False)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', required=True)
    run(parser.parse_args().image)
