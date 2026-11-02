#!/usr/bin/env python3
"""Real local TLS and proxy acceptance; temporary CA, synthetic accounts/upstream only."""
import argparse
from contextlib import contextmanager
from http.cookiejar import CookieJar
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
from live_acceptance import port, wait


def stop(process):
    if process is not None and process.poll() is None:
        process.terminate()
        process.wait(timeout=20)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


class Client:
    def __init__(self, base, context):
        self.base = base
        self.context = context
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context),
            urllib.request.HTTPCookieProcessor(CookieJar()), NoRedirect())

    def open(self, path, value=None, headers=None):
        body = json.dumps(value).encode() if value is not None else None
        request = urllib.request.Request(self.base + path, data=body,
            headers={'Origin': self.base, 'Content-Type': 'application/json', **(headers or {})})
        return self.opener.open(request, timeout=5)

    def json(self, path, value=None):
        with self.open(path, value) as response:
            return json.load(response)

    def login(self, username, password):
        with self.open('/auth/login', {'username': username, 'password': password}) as response:
            cookie = SimpleCookie()
            for header in response.headers.get_all('Set-Cookie', []):
                cookie.load(header)
            assert cookie['codearts_session']['secure'], 'HTTPS login cookie is missing Secure'
            assert cookie['codearts_session']['httponly']
            assert cookie['codearts_session']['samesite'] == 'Strict'
            return json.load(response)['user']['role']


@contextmanager
def proxy(caddy, root, upstream):
    tls_port, http_port = port(), port()
    base = f'https://localhost:{tls_port}'
    # Only the test's listener/CA settings differ. No trust store modifications or ACME traffic.
    template = (Path(ops.__file__).parent / 'Caddyfile').read_text()
    config = root / 'Caddyfile'
    config.write_text(template.replace('admin off', f'admin off\n\tskip_install_trust\n'
        f'\tdefault_bind 127.0.0.1\n\thttp_port {http_port}'))
    env = {**os.environ, 'PANEL_DOMAIN': base, 'PANEL_UPSTREAM': upstream,
           'XDG_DATA_HOME': str(root / 'caddy-data'), 'XDG_CONFIG_HOME': str(root / 'caddy-config')}
    process = None
    with (root / 'caddy.log').open('wb') as log:
        try:
            adapted = subprocess.run([caddy, 'adapt', '--config', str(config), '--adapter', 'caddyfile'],
                env=env, capture_output=True, text=True, check=True, timeout=10)
            settings = json.loads(adapted.stdout)
            for server in settings['apps']['http']['servers'].values():
                assert not server.get('write_timeout')
            process = subprocess.Popen([caddy, 'run', '--config', str(config), '--adapter', 'caddyfile'],
                env=env, stdout=log, stderr=log)
            ca = root / 'caddy-data/caddy/pki/authorities/local/root.crt'
            for _ in range(200):
                assert process.poll() is None, 'temporary Caddy exited; inspect isolated log'
                if ca.exists():
                    context = ssl.create_default_context(cafile=str(ca))
                    client = Client(base, context)
                    try:
                        with client.open('/'):
                            break
                    except (OSError, ValueError):
                        pass
                time.sleep(.05)
            else:
                raise AssertionError('local TLS endpoint did not start')
            # A trusted CA is required; do not hide certificate failures with verify=False.
            untrusted = Client(base, ssl.create_default_context())
            try:
                untrusted.json('/setup/status')
                raise AssertionError('temporary CA was unexpectedly globally trusted')
            except urllib.error.URLError as error:
                assert isinstance(error.reason, ssl.SSLCertVerificationError)
            yield client, http_port
        finally:
            stop(process)


def app_acceptance(binary, caddy, root):
    checks = []
    data = root / 'data'
    data.mkdir(mode=0o700)
    app_port = port()
    env = {k: v for k, v in os.environ.items() if not k.startswith('CODEARTS_')}
    with (root / 'app.log').open('wb') as log:
        app = subprocess.Popen([str(binary), '-host', '127.0.0.1', '-port', str(app_port),
            '-data', str(data / 'state.json'), '-no-import', '-no-refresh'], env=env, stdout=log, stderr=log)
        try:
            wait(f'http://127.0.0.1:{app_port}', 'setup_required')
            with proxy(caddy, root, f'127.0.0.1:{app_port}') as (client, http_port):
                assert client.json('/setup/status')['needs_setup']
                token = json.loads((data / 'state.json.setup.json').read_text())['setup_token']
                password = secrets.token_urlsafe(24)
                assert client.json('/setup/install', {'setup_token': token, 'password': password,
                    'confirm_password': password})['ok']
                wait(f'http://127.0.0.1:{app_port}', 'running')
                checks.append('verified_tls_first_install_through_proxy')
                assert client.login('admin', password) == 'admin'
                checks.append('initial_admin_https_login_sets_secure_session_cookie')
                try:
                    client.opener.open(f'http://localhost:{http_port}/auth/session', timeout=3)
                    raise AssertionError('HTTP did not redirect to HTTPS')
                except urllib.error.HTTPError as response:
                    assert response.code == 308
                    assert response.headers['Location'] == client.base + '/auth/session', response.headers['Location']
                checks.append('http_redirects_to_https')
                assert client.json('/auth/session')['user']['role'] == 'admin'
                with client.open('/') as response:
                    assert b'<html' in response.read().lower()
                checks.append('https_spa_and_admin_session')
                client.json('/admin/api/consumer/identity', {'settings': {'public_origin': client.base}})
                group = client.json('/admin/api/groups', {'name': 'd2-synthetic-group', 'account_ids': []})['group']['id']
                user_password = secrets.token_urlsafe(24)
                client.json('/admin/api/users', {'username': 'd2-user', 'password': user_password,
                    'group_id': group, 'enabled': True})
                user = Client(client.base, client.context)
                assert user.login('d2-user', user_password) == 'user'
                assert user.json('/auth/session')['user']['role'] == 'user'
                try:
                    user.json('/admin/api/users')
                    raise AssertionError('ordinary user accessed admin API through proxy')
                except urllib.error.HTTPError as response:
                    assert response.code == 403
                checks.append('https_user_login_retains_role_boundary')
                try:
                    with client.open('/auth/login', {'username': 'admin', 'password': password},
                        {'Origin': 'https://untrusted.invalid'}):
                        raise AssertionError('cross-origin login was accepted')
                except urllib.error.HTTPError as response:
                    assert response.code == 403
                checks.append('proxy_preserves_same_origin_rejection')
        finally:
            stop(app)
    return checks


def stream_acceptance(caddy, root):
    checks, timings = [], []
    error_calls = {}
    cancelled = threading.Event()
    first = 'data: {"text":"第一段","id":9007199254740993}\n\n'.encode()
    second = b'data: {"type":"tool_result","ok":true}\n\ndata: [DONE]\n\n'
    static = b'body { color: #123456; }\n' * 100

    class Upstream(BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.1'
        def log_message(self, *args): pass
        def do_POST(self):
            self.rfile.read(int(self.headers.get('Content-Length', 0)))
            self.do_GET()
        def do_GET(self):
            if self.path in ('/v1/status/429', '/v1/status/500'):
                error_calls[self.path] = error_calls.get(self.path, 0) + 1
                body = b'{"error":{"message":"synthetic upstream error"}}'
                self.send_response(int(self.path.rsplit('/', 1)[1]))
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.send_header('Retry-After', '7')
                self.end_headers()
                self.wfile.write(body)
                return
            elif self.path in ('/', '/headers'):
                body = json.dumps(dict(self.headers)).encode()
                kind = 'application/json'
            elif self.path.startswith('/assets/'):
                body, kind = static, 'text/css'
            else:
                self.send_response(200)
                self.send_header('Content-Type', 'text/event-stream')
                self.send_header('Connection', 'close')
                self.end_headers()
                try:
                    self.wfile.write(first)
                    self.wfile.flush()
                    if self.path != '/cancel':
                        time.sleep(.8)
                        self.wfile.write(second)
                        self.wfile.flush()
                    else:
                        for _ in range(200):
                            self.wfile.write(b'data: ' + b'x' * 16384 + b'\n\n')
                            self.wfile.flush()
                            time.sleep(.02)
                except (BrokenPipeError, ConnectionResetError):
                    if self.path == '/cancel':
                        cancelled.set()
                return
            self.send_response(200)
            self.send_header('Content-Type', kind)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    mock = ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
    thread = threading.Thread(target=mock.serve_forever, daemon=True)
    thread.start()
    try:
        with proxy(caddy, root, f'127.0.0.1:{mock.server_port}') as (client, _):
            spoofed = {'X-Real-IP': '1.2.3.4', 'X-Forwarded-For': '1.2.3.4', 'X-Forwarded-Proto': 'http',
                'X-Forwarded-Host': 'untrusted.invalid', 'Forwarded': 'for=1.2.3.4', 'CF-Connecting-IP': '1.2.3.4'}
            with client.open('/headers', headers=spoofed) as response:
                headers = {k.lower(): v for k, v in json.load(response).items()}
            assert headers['host'] == client.base.removeprefix('https://')
            assert headers['x-forwarded-host'] == headers['host']
            assert headers['x-forwarded-proto'] == 'https'
            assert headers['x-real-ip'] == headers['x-forwarded-for'] == '127.0.0.1'
            assert 'forwarded' not in headers and 'cf-connecting-ip' not in headers
            checks.append('https_host_and_spoofed_forwarding_headers')
            for code in (429, 500):
                path = f'/v1/status/{code}'
                try:
                    with client.open(path, {'stream': True}):
                        raise AssertionError('proxy swallowed upstream error')
                except urllib.error.HTTPError as response:
                    assert response.code == code
                    assert response.headers['Retry-After'] == '7'
                    assert response.read() == b'{"error":{"message":"synthetic upstream error"}}'
                assert error_calls[path] == 1
            checks.append('upstream_errors_and_retry_after_preserved_without_proxy_retries')
            for path in ('/v1/chat/completions', '/v1/responses', '/v1/messages', '/v1/search/messages'):
                begin = time.monotonic()
                with client.open(path, {'stream': True}, {'Accept-Encoding': 'gzip'}) as response:
                    assert not response.headers.get('Content-Encoding')
                    assert response.readline() == first.splitlines(keepends=True)[0]
                    elapsed = time.monotonic() - begin
                    assert elapsed < .6, (path, elapsed)
                    assert response.read() == b'\n' + second
                timings.append({'path': path, 'first_frame_seconds': round(elapsed, 4)})
            checks.append('four_https_sse_paths_flush_without_compression_or_byte_changes')
            response = client.open('/cancel')
            assert response.readline() == first.splitlines(keepends=True)[0]
            response.close()
            assert cancelled.wait(3), 'client cancellation did not reach upstream'
            checks.append('https_disconnect_cancels_upstream')
            with client.open('/assets/test.css', headers={'Accept-Encoding': 'gzip'}) as response:
                import gzip
                assert response.headers.get('Content-Encoding') == 'gzip'
                assert gzip.decompress(response.read()) == static
            checks.append('static_assets_compress_while_sse_does_not')
    finally:
        mock.shutdown()
        mock.server_close()
        thread.join()
    return checks, timings


def run(archive, caddy):
    with tempfile.TemporaryDirectory(prefix='codearts-d2-') as temporary:
        root = Path(temporary)
        release = root / 'release'
        ops.unpack(archive, release, 'release', ops.sha256(archive))
        ops.check_release(release)
        app_root, mock_root = root / 'app', root / 'mock'
        app_root.mkdir(mode=0o700)
        mock_root.mkdir(mode=0o700)
        checks = app_acceptance(release / 'codearts2api-panel', caddy, app_root)
        stream_checks, timings = stream_acceptance(caddy, mock_root)
        checks.extend(stream_checks)
    print(json.dumps({'passed': True, 'checks': checks, 'timings': timings,
        'real_provider_requests': 0, 'global_trust_store_changed': False}, indent=2))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    parser.add_argument('--caddy', required=True)
    args = parser.parse_args()
    run(args.archive.resolve(), str(Path(args.caddy).resolve()))
