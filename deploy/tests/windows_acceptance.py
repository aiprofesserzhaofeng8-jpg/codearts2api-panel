#!/usr/bin/env python3
"""Exercise the released Windows launcher, real HTTP auth, and persistent state."""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile


def run(archive):
    if os.name != 'nt':
        raise RuntimeError('Native Windows acceptance must run on Windows')
    archive = archive.resolve()
    checksum = archive.with_name(archive.name + '.sha256').read_text(encoding='ascii').split()[0]
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == checksum
    with tempfile.TemporaryDirectory(prefix='panel Windows 验收 ') as folder:
        root = Path(folder)
        first, second = root / '首次 解压', root / '升级 解压'
        with zipfile.ZipFile(archive) as package:
            manifest = json.loads(package.read('manifest.json'))
            assert manifest['os'] == 'windows' and manifest['arch'] == 'amd64'
            expected = {'codearts2api-panel.exe', 'start.cmd', 'README.md', 'LICENSE', 'LICENSE.workbuddy', 'LICENSE.qoder', 'LICENSE.kiro'}
            assert set(manifest['files']) == expected
            assert set(package.namelist()) == expected | {'manifest.json'}
            for name, entry in manifest['files'].items():
                payload = package.read(name)
                assert len(payload) == entry['size']
                assert hashlib.sha256(payload).hexdigest() == entry['sha256']
            package.extractall(first)
            package.extractall(second)
        version = subprocess.check_output([str(first / 'codearts2api-panel.exe'), '-version'], text=True).strip()
        assert version == 'codearts2api ' + manifest['version']
        # The launcher must work from a different cwd and from paths containing spaces/Unicode.
        local = root / '用户 数据'
        local.mkdir()
        cwd = root / 'unrelated cwd'
        cwd.mkdir()
        state = local / 'CodeArts2API/state.json'
        token_path = Path(str(state) + '.setup.json')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        env = {key: value for key, value in os.environ.items() if not key.upper().startswith('CODEARTS_')}
        env.update(LOCALAPPDATA=str(local), CODEARTS_PORT=str(port))
        client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        process = None
        log_path = root / 'private-test.log'
        log = log_path.open('wb')

        def request(path, body=None, origin=None):
            headers = {'Content-Type': 'application/json', 'Origin': origin or base}
            req = urllib.request.Request(base + path, headers=headers,
                                         data=json.dumps(body).encode() if body is not None else None)
            try:
                with client.open(req, timeout=5) as response:
                    return response.status, response.read()
            except urllib.error.HTTPError as error:
                return error.code, error.read()

        def wait_ready(installed):
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError('Windows launcher exited before readiness')
                try:
                    status, body = request('/setup/status')
                    if status == 200 and json.loads(body)['ready'] == installed:
                        return
                except OSError:
                    pass
                time.sleep(.1)
            raise TimeoutError('Windows application did not become ready')

        def start(directory, installed):
            nonlocal process
            comspec = os.environ['COMSPEC']
            command = f'"{comspec}" /d /s /c ""{directory / "start.cmd"}" -no-refresh"'
            process = subprocess.Popen(command, cwd=cwd, env=env, stdout=log, stderr=log,
                                       creationflags=subprocess.CREATE_NEW_PROCESS_GROUP)
            wait_ready(installed)

        def stop():
            nonlocal process
            if process is not None and process.poll() is None:
                try:
                    # Go maps CTRL_BREAK_EVENT to os.Interrupt, allowing runtime.Close to flush state.
                    process.send_signal(signal.CTRL_BREAK_EVENT)
                    process.wait(timeout=20)
                except BaseException:
                    subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                    raise
            process = None

        def login(username, password):
            status, _ = request('/auth/login', {'username': username, 'password': password})
            assert status == 200, f'Login failed: {status}'

        try:
            start(first, False)
            assert not state.exists()
            email='owner@example.com'
            assert request('/admin/api/state')[0] == 503
            stop()
            start(first, False)
            assert json.loads(token_path.read_text(encoding='utf-8')) == {'pending': True}
            password = secrets.token_urlsafe(24)
            body = {'email': email, 'password': password, 'confirm_password': password}
            assert request('/setup/install', {**body, 'email': 'invalid'})[0] == 400
            assert request('/setup/install', body, 'https://foreign.example')[0] == 403
            assert request('/setup/install', body)[0] == 200
            wait_ready(True)
            assert state.is_file() and not token_path.exists()
            assert password not in state.read_text(encoding='utf-8')
            assert request('/setup/install', body)[0] == 403
            assert request('/v1/models')[0] == 401
            status, html = request('/')
            assert status == 200
            asset = re.search(rb'(?:src|href)="(/assets/[^" ]+\.js)"', html)[1].decode()
            assert request(asset)[0] == 200
            login('owner@example.com', password)
            assert request('/admin/api/state')[0] == 200
            status, payload = request('/admin/api/groups', {'name': 'Windows persistence', 'account_ids': [], 'model_ids': []})
            assert status == 200
            group_id = json.loads(payload)['group']['id']
            user_password = secrets.token_urlsafe(24)
            assert request('/admin/api/users', {'username': 'windows-test', 'password': user_password, 'group_id': group_id, 'enabled': True})[0] == 200
            stop()
            # Extracted program locations change on upgrade; launcher data must remain fixed.
            start(second, True)
            login('owner@example.com', password)
            status, payload = request('/admin/api/groups')
            assert status == 200 and any(group['id'] == group_id for group in json.loads(payload)['groups'])
            login('windows-test', user_password)
            assert request('/user/api/profile')[0] == 200
            assert request('/admin/api/state')[0] == 403
            assert request('/setup/install', body)[0] == 403
            assert not list(cwd.glob('*.json')) and not list(first.glob('state*')) and not list(second.glob('state*'))
            stop()
            log.flush()
            logs = log_path.read_bytes()
            assert password.encode() not in logs and user_password.encode() not in logs
            assert logs.count('已退出'.encode()) == 2, 'Both installed runs must flush state and exit gracefully'
        finally:
            stop()
            log.close()
    print(json.dumps({'passed': True, 'version': manifest['version'], 'platform': 'windows-amd64',
                      'checks': ['zip_checksums', 'native_exe_version', 'packaged_cmd_launcher',
                                 'spaces_unicode_and_unrelated_cwd', 'pending_setup_survives_restart',
                                 'setup_authentication_and_origin', 'embedded_ui', 'admin_and_user_login',
                                 'role_isolation', 'state_reused_after_program_directory_change',
                                 'graceful_shutdown', 'no_secret_logs'], 'real_provider_requests': 0}, indent=2))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, required=True)
    run(parser.parse_args().archive)
