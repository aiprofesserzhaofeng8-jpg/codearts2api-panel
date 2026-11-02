#!/usr/bin/env python3
"""Disposable real-process acceptance. No existing state or provider accounts."""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops


def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def post(base, path, value):
    request = urllib.request.Request(base + path, data=json.dumps(value).encode(),
                                     headers={'Content-Type': 'application/json', 'Origin': base})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=10) as response:
        return json.load(response)


def wait(base, expected):
    for _ in range(100):
        if ops.health(base) == expected:
            return
        time.sleep(.05)
    raise AssertionError('health state not reached: ' + expected)


def run(archive, caddy=None):
    checks = []
    with tempfile.TemporaryDirectory(prefix='codearts-deploy-') as temporary:
        root = Path(temporary)
        release = root / 'release'
        ops.unpack(archive, release, 'release', ops.sha256(archive))
        meta = ops.check_release(release)
        data = root / 'data'
        data.mkdir(mode=0o700)
        app_port = port()
        base = f'http://127.0.0.1:{app_port}'
        env = {k: v for k, v in os.environ.items() if not k.startswith('CODEARTS_')}
        process = None
        log = (root / 'server.log').open('wb')
        def start(directory):
            return subprocess.Popen([str(release / 'codearts2api-panel'), '-host', '127.0.0.1', '-port', str(app_port),
                                     '-data', str(directory / 'state.json'), '-no-import', '-no-refresh'], env=env,
                                    stdout=log, stderr=log)
        def stop(child):
            if child and child.poll() is None:
                child.terminate()
                child.wait(timeout=20)
        try:
            process = start(data)
            wait(base, 'setup_required')
            token = json.loads((data / 'state.json.setup.json').read_text())['setup_token']
            password = secrets.token_urlsafe(24)
            assert post(base, '/setup/install', {'setup_token': token, 'password': password, 'confirm_password': password})['ok']
            wait(base, 'running')
            assert post(base, '/auth/login', {'username': 'admin', 'password': password})['user']['role'] == 'admin'
            assert not (data / 'state.json.setup.json').exists()
            checks.append('real_binary_install_health_and_admin_login')
            try:
                ops.no_writer(data)
                raise AssertionError('live data was accepted for offline backup')
            except ops.OpsError:
                pass
            checks.append('running_process_blocks_offline_backup')
            stop(process)
            snapshot = json.loads((data / 'state.json').read_text())
            # Model a durably settled request whose snapshot has not yet caught up.
            # This is a synthetic journal fixture, never a real provider request.
            sequence = snapshot.get('usage_sequence', 0) + 1
            journal_entry = {'sequence': sequence,
                             'key_hash': hashlib.sha256(snapshot['keys'][0]['key'].encode()).hexdigest(),
                             'entry': {'request_id': 'd1-synthetic-settlement', 'time': int(time.time()),
                                       'model': 'd1-synthetic-model', 'status': 200, 'attempts': 1,
                                       'output_len': 2,
                                       'usage': {'known': True, 'input_tokens': 12, 'output_tokens': 4},
                                       'estimated_cost': {'currency': 'USD', 'amount': 0.125}}, 'out_chars': 2}
            journal = data / 'state.json.usage.jsonl'
            with journal.open('w') as stream:
                journal.chmod(0o600)
                stream.write(json.dumps(journal_entry) + '\n')
                stream.flush()
                os.fsync(stream.fileno())
            backup = root / 'backup.tar.gz'
            with ops.stopped(data, offline=True):
                ops.backup(data, backup)
            restored = root / 'restored'
            ops.unpack(backup, restored, 'backup')
            assert ops.inventory(restored) == ops.inventory(data)
            process = start(restored)
            wait(base, 'running')
            assert post(base, '/auth/login', {'username': 'admin', 'password': password})['user']['role'] == 'admin'
            stop(process)
            resumed = json.loads((restored / 'state.json').read_text())
            assert resumed['version'] == snapshot['version'] == meta['schema']
            assert [key['key'] for key in resumed['keys']] == [key['key'] for key in snapshot['keys']]
            assert resumed['usage_sequence'] == sequence
            assert resumed['stats']['total_requests'] == snapshot['stats']['total_requests'] + 1
            assert resumed['stats']['estimated_usd'] == snapshot['stats']['estimated_usd'] + 0.125
            old_usage = snapshot['keys'][0]['budget_usage']
            new_usage = resumed['keys'][0]['budget_usage']
            assert new_usage['requests'] == old_usage['requests'] + 1
            assert new_usage['tokens'] == old_usage['tokens'] + 16
            assert new_usage['usd'] == old_usage['usd'] + 0.125
            checks.append('backup_restore_restart_preserves_authentication_and_keys')
            process = start(restored)
            wait(base, 'running')
            stop(process)
            twice = json.loads((restored / 'state.json').read_text())
            assert twice['stats']['total_requests'] == resumed['stats']['total_requests']
            assert twice['stats']['estimated_usd'] == resumed['stats']['estimated_usd']
            assert twice['keys'][0]['budget_usage'] == new_usage
            assert twice['usage_sequence'] == sequence
            checks.append('nonempty_journal_replayed_exactly_once_after_restore')
            if caddy:
                checks.extend(proxy_acceptance(caddy, base, root, lambda: start(restored), stop, password))
        finally:
            stop(process)
            log.close()
    print(json.dumps({'passed': True, 'checks': checks, 'real_provider_requests': 0}, indent=2))


def proxy_acceptance(caddy, base, root, start_app, stop_app, password):
    checks = []
    proxy_port = port()
    proxy_base = f'http://127.0.0.1:{proxy_port}'
    template = Path(ops.__file__).parent / 'Caddyfile'
    env = {**os.environ, 'PANEL_DOMAIN': proxy_base, 'PANEL_UPSTREAM': base.removeprefix('http://'),
           'XDG_DATA_HOME': str(root / 'caddy-data'), 'XDG_CONFIG_HOME': str(root / 'caddy-config')}
    log = (root / 'caddy.log').open('wb')
    def start_proxy(upstream):
        return subprocess.Popen([caddy, 'run', '--config', str(template), '--adapter', 'caddyfile'],
                                env={**env, 'PANEL_UPSTREAM': upstream}, stdout=log, stderr=log)
    app = start_app()
    proxy = None
    try:
        wait(base, 'running')
        proxy = start_proxy(base.removeprefix('http://'))
        wait(proxy_base, 'running')
        browser = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor())
        request = urllib.request.Request(proxy_base + '/auth/login',
            data=json.dumps({'username': 'admin', 'password': password}).encode(),
            headers={'Content-Type': 'application/json', 'Origin': proxy_base})
        with browser.open(request, timeout=10) as response:
            assert json.load(response)['user']['role'] == 'admin'
        with browser.open(proxy_base + '/auth/session', timeout=10) as response:
            assert json.load(response)['authenticated'], 'local HTTP session cookie must remain usable'
        checks.append('real_caddy_same_origin_admin_login')
    finally:
        stop_app(proxy)
        stop_app(app)
    cancel_seen = threading.Event()
    class Stream(BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.1'
        def log_message(self, *args): pass
        def do_GET(self):
            if self.path == '/headers':
                body = json.dumps(dict(self.headers)).encode()
                self.send_response(200)
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Connection', 'close')
            self.end_headers()
            try:
                self.wfile.write(b'data: first\n\n')
                self.wfile.flush()
                if self.path == '/stream':
                    time.sleep(.8)
                    self.wfile.write(b'data: second\n\n')
                    self.wfile.flush()
                else:
                    for _ in range(200):
                        self.wfile.write(b'data: ' + b'x' * 16384 + b'\n\n')
                        self.wfile.flush()
                        time.sleep(.02)
            except (BrokenPipeError, ConnectionResetError):
                cancel_seen.set()
    mock = ThreadingHTTPServer(('127.0.0.1', 0), Stream)
    thread = threading.Thread(target=mock.serve_forever, daemon=True)
    thread.start()
    try:
        proxy = start_proxy(f'127.0.0.1:{mock.server_port}')
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        for _ in range(100):
            try:
                with opener.open(proxy_base + '/headers', timeout=1):
                    break
            except OSError:
                time.sleep(.05)
        request = urllib.request.Request(proxy_base + '/stream', headers={'Accept-Encoding': 'gzip'})
        begin = time.monotonic()
        with opener.open(request, timeout=3) as response:
            assert not response.headers.get('Content-Encoding')
            assert response.readline() == b'data: first\n'
            first = time.monotonic() - begin
            assert first < .6, first
            assert b'data: second' in response.read()
        checks.append('real_caddy_sse_first_frame_before_completion_without_compression')
        response = opener.open(proxy_base + '/cancel', timeout=3)
        assert response.readline() == b'data: first\n'
        response.close()
        assert cancel_seen.wait(3), 'proxy did not cancel upstream'
        checks.append('real_caddy_client_disconnect_cancels_upstream')
        request = urllib.request.Request(proxy_base + '/headers', headers={'X-Forwarded-For': '1.2.3.4', 'Forwarded': 'for=1.2.3.4', 'CF-Connecting-IP': '1.2.3.4'})
        with opener.open(request) as response:
            headers = {k.lower(): v for k, v in json.load(response).items()}
        assert headers['host'] == f'127.0.0.1:{proxy_port}'
        assert headers['x-forwarded-for'] == '127.0.0.1'
        assert 'forwarded' not in headers and 'cf-connecting-ip' not in headers
        checks.append('real_caddy_host_preservation_and_spoofed_header_removal')
    finally:
        stop_app(proxy)
        mock.shutdown()
        mock.server_close()
        thread.join()
        log.close()
    return checks


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--caddy', help='Optional local Caddy executable for proxy/SSE/cancellation acceptance')
    args = parser.parse_args()
    run(args.archive.resolve(), args.caddy)
