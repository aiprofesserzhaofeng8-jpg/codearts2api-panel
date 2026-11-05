import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import threading
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops


class OperationsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.data = self.root / 'data'
        self.data.mkdir(mode=0o700)
        (self.data / 'state.json').write_text(json.dumps({'version': 10, 'credential': 'fixture-current'}))
        for suffix in ops.SIDECARS:
            (self.data / ('state.json' + suffix)).write_text('synthetic-' + suffix)
        self.archive = self.root / 'backup.tar.gz'

    def make_archive(self, entries, kind='backup', **extra):
        files = {name: {'size': len(body), 'sha256': hashlib.sha256(body).hexdigest()} for name, body in entries.items()}
        meta = {'format': 1, 'kind': kind, 'files': files, **extra}
        archive = self.root / 'custom.tar.gz'
        with tarfile.open(archive, 'w:gz') as tar:
            for name, body in {'manifest.json': json.dumps(meta).encode(), **{'data/' + k: v for k, v in entries.items()}}.items():
                info = tarfile.TarInfo(name)
                info.size = len(body)
                tar.addfile(info, io.BytesIO(body))
        return archive

    def test_backup_restore_all_sidecars_and_permissions(self):
        meta = ops.backup(self.data, self.archive)
        self.assertEqual(meta['schema'], 10)
        target = self.root / 'restored'
        ops.unpack(self.archive, target, 'backup')
        self.assertEqual(ops.inventory(self.data), ops.inventory(target))
        self.assertEqual(self.archive.stat().st_mode & 0o777, 0o600)
        self.assertEqual(target.stat().st_mode & 0o777, 0o700)
        self.assertTrue(all(p.stat().st_mode & 0o777 == 0o600 for p in target.iterdir()))
        with self.assertRaises(ops.OpsError):
            ops.unpack(self.archive, self.data, 'backup')
        self.assertIn('fixture-current', (self.data / 'state.json').read_text())

    def test_pending_install_and_custom_state_name(self):
        for p in self.data.iterdir():
            p.unlink()
        (self.data / 'custom.json.setup.json').write_text('synthetic')
        meta = ops.backup(self.data, self.archive, 'custom.json')
        self.assertIsNone(meta['schema'])

    def test_malformed_state_refuses_backup_without_exposing_content(self):
        for value in ([], {'version': 'private-secret'}, {'version': True}, {'version': -1}):
            (self.data / 'state.json').write_text(json.dumps(value))
            with self.assertRaises(ops.OpsError) as error:
                ops.backup(self.data, self.archive)
            self.assertNotIn('private-secret', str(error.exception))
            self.assertFalse(self.archive.exists())

    def test_symlink_backup_rejected(self):
        (self.data / 'escape').symlink_to('/etc/passwd')
        with self.assertRaises(ops.OpsError):
            ops.backup(self.data, self.archive)
        self.assertFalse(self.archive.exists())

    def test_doctor_redacts_contents_and_does_not_modify_data(self):
        before = ops.inventory(self.data)
        args = ops.parser().parse_args(['doctor', '--data-dir', str(self.data)])
        with patch.object(ops, 'health', return_value='running'), patch('sys.stdout', new_callable=io.StringIO) as output:
            ops.doctor(args)
        report = json.loads(output.getvalue())
        self.assertEqual(report['schema'], 10)
        self.assertNotIn('fixture-current', output.getvalue())
        self.assertEqual(ops.inventory(self.data), before)

    def test_unsafe_restore_paths(self):
        for name in ('../../escape', '/absolute', 'a/../escape', 'a//x', 'a\\x'):
            with self.subTest(name=name):
                archive = self.make_archive({name: b'secret'})
                with self.assertRaises(ops.OpsError):
                    ops.unpack(archive, self.root / 'restored', 'backup')
                self.assertFalse((self.root / 'restored').exists())

    def test_checksum_and_extra_file_rejected(self):
        archive = self.make_archive({'state.json': b'original'})
        with tarfile.open(archive, 'r:gz') as tar:
            manifest = tar.extractfile('manifest.json').read()
        for entries in ({'data/state.json': b'tampered'}, {'data/state.json': b'original', 'data/extra': b'x'}):
            with tarfile.open(archive, 'w:gz') as tar:
                for name, body in {'manifest.json': manifest, **entries}.items():
                    info = tarfile.TarInfo(name)
                    info.size = len(body)
                    tar.addfile(info, io.BytesIO(body))
            with self.assertRaises(ops.OpsError):
                ops.unpack(archive, self.root / 'restored', 'backup')
            self.assertFalse((self.root / 'restored').exists())

    def test_links_duplicates_and_limit_rejected(self):
        for mode in ('symlink', 'duplicate', 'large'):
            archive = self.make_archive({'state.json': b'x'})
            with tarfile.open(archive, 'r:gz') as tar:
                manifest = tar.extractfile('manifest.json').read()
            with tarfile.open(archive, 'w:gz') as tar:
                item = tarfile.TarInfo('manifest.json')
                item.size = len(manifest)
                tar.addfile(item, io.BytesIO(manifest))
                item = tarfile.TarInfo('data/state.json')
                if mode == 'symlink':
                    item.type, item.linkname = tarfile.SYMTYPE, '/etc/passwd'
                    tar.addfile(item)
                else:
                    item.size = 1
                    tar.addfile(item, io.BytesIO(b'x'))
                    if mode == 'duplicate':
                        tar.addfile(item, io.BytesIO(b'x'))
            with patch.object(ops, 'MAX_BYTES', 1 if mode == 'large' else ops.MAX_BYTES):
                with self.assertRaises(ops.OpsError):
                    ops.unpack(archive, self.root / 'restored', 'backup')

    def test_active_writer_detected(self):
        child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(20)', '-data', str(self.data / 'state.json')])
        try:
            with self.assertRaises(ops.OpsError):
                ops.no_writer(self.data)
        finally:
            child.terminate()
            child.wait()

    def test_backup_failure_restarts_original_service(self):
        class FakeService:
            active = True
            calls = []
            def running(self): return self.active
            def stop(self): self.active = False; self.calls.append('stop')
            def start(self): self.active = True; self.calls.append('start')
        service = FakeService()
        with self.assertRaises(RuntimeError):
            with ops.stopped(self.data, service):
                raise RuntimeError('fixture')
        self.assertEqual(service.calls, ['stop', 'start'])
        service.calls, service.active = [], False
        with ops.stopped(self.data, service):
            pass
        self.assertEqual(service.calls, ['stop'])

    def test_backup_inside_data_and_changes_rejected(self):
        with self.assertRaises(ops.OpsError):
            ops.backup(self.data, self.data / 'backup.tar.gz')
        actual = ops.inventory
        calls = 0
        def changing(path):
            nonlocal calls
            result = actual(path)
            calls += 1
            if calls > 1:
                result['new'] = {'size': 1, 'sha256': 'x'}
            return result
        with patch.object(ops, 'inventory', changing), self.assertRaises(ops.OpsError):
            ops.backup(self.data, self.archive)
        self.assertFalse(self.archive.exists())

    def test_install_verifies_package_before_creating_service(self):
        arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}[ops.platform.machine()]
        archive = self.make_archive({'codearts2api-panel': b'#!/bin/sh\necho codearts2api v1.0.0\n'},
                                    kind='release', version='v1.0.0', os='linux', arch=arch, schema=10)
        root = self.root / 'installed'
        args = ops.parser().parse_args(['install', '--root', str(root), '--archive', str(archive),
                                        '--sha256', '0' * 64, '--no-start'])
        with patch.dict(os.environ, {'XDG_CONFIG_HOME': str(self.root / 'config')}):
            with self.assertRaises(ops.OpsError):
                ops.install(args)
            self.assertFalse(root.exists())
            args.sha256 = ops.sha256(archive)
            ops.install(args)
            self.assertEqual((root / 'current').resolve().name, 'v1.0.0')
            self.assertEqual(list((root / 'data').iterdir()), [])
            unit = (self.root / 'config/systemd/user/codearts2api-managed.service').read_text()
            self.assertIn('-host 127.0.0.1', unit)
            self.assertNotIn('password', unit)
            with self.assertRaises(ops.OpsError):
                ops.install(args)

    def test_wrong_arch_and_version_rejected(self):
        folder = self.root / 'release'
        folder.mkdir()
        for meta in ({'version': 'v1.0.0', 'os': 'linux', 'arch': 'invalid'}, {'version': 'latest'}):
            (folder / 'manifest.json').write_text(json.dumps(meta))
            with self.assertRaises(ops.OpsError):
                ops.check_release(folder)

    def test_install_port_conflict_does_not_create_installation(self):
        import socket
        root = self.root / 'blocked-install'
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            listener.listen()
            args = ops.parser().parse_args(['install', '--root', str(root), '--name', 'port-conflict-fixture',
                                            '--port', str(listener.getsockname()[1]), '--archive', str(self.archive), '--sha256', '0' * 64])
            with patch.dict(os.environ, {'XDG_CONFIG_HOME': str(self.root / 'config')}):
                with self.assertRaisesRegex(ops.OpsError, '端口'):
                    ops.install(args)
            self.assertFalse(root.exists())


class HealthTest(unittest.TestCase):
    def test_health_modes_and_redaction(self):
        class Handler(BaseHTTPRequestHandler):
            mode = 'pending'
            def log_message(self, *args): pass
            def do_GET(self):
                body, status = {}, 503
                if self.mode == 'running' and self.path == '/health':
                    body, status = {'service': 'codearts2api-panel', 'status': 'ok'}, 200
                elif self.mode == 'pending' and self.path == '/setup/status':
                    body, status = {'needs_setup': True, 'ready': False, 'admin_username': 'admin'}, 200
                elif self.mode == 'wrong':
                    body, status = {'service': 'another', 'status': 'ok'}, 200
                self.send_response(status)
                self.end_headers()
                self.wfile.write(json.dumps(body).encode())
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f'http://127.0.0.1:{server.server_port}'
            script = str(Path(ops.__file__).parent / 'healthcheck.sh')
            for mode, expected, live, ready in [('pending', 'setup_required', 0, 1), ('running', 'running', 0, 0), ('wrong', 'unhealthy', 1, 1)]:
                Handler.mode = mode
                self.assertEqual(ops.health(url), expected)
                for extra, code in [([], live), (['--ready'], ready)]:
                    result = subprocess.run(['sh', script] + extra, env={**os.environ, 'CODEARTS_HEALTH_URL': url}, capture_output=True)
                    self.assertEqual(result.returncode, code, result.stdout)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

class UpgradeTest(unittest.TestCase):
    setUp = OperationsTest.setUp
    make_archive = OperationsTest.make_archive
    def test_operation_lock_prevents_overlapping_tools(self):
        with ops.operation_lock(self.data):
            with self.assertRaises(ops.OpsError):
                with ops.operation_lock(self.data):
                    pass

    def upgrade_fixture(self, version='v1.0.1', schema=10):
        process_check = patch.object(ops, 'verify_process')
        process_check.start()
        self.addCleanup(process_check.stop)
        root = self.root / 'managed'
        old = root / 'releases/v1.0.0'
        old.mkdir(parents=True)
        (old / 'codearts2api-panel').write_text('old-binary')
        (old / 'manifest.json').write_text(json.dumps({'version': 'v1.0.0', 'schema': 10}))
        (root / 'current').symlink_to('releases/v1.0.0')
        (root / 'backups').mkdir()
        (root / 'data').mkdir()
        (root / 'data/state.json').write_text(json.dumps({'version': 10, 'token': 'current'}))
        (root / 'installation.json').write_text(json.dumps({'unit': 'fixture.service', 'port': 10087}))
        archive = self.make_archive({'codearts2api-panel': b'candidate'}, kind='release', version=version, schema=schema)
        args = ops.parser().parse_args(['upgrade', '--root', str(root), '--archive', str(archive), '--sha256', ops.sha256(archive)])
        class FakeService:
            active = True
            calls = []
            def running(self): return self.active
            def stop(self): self.active = False; self.calls.append('stop')
            def start(self): self.active = True; self.calls.append('start')
        return root, args, FakeService()

    def test_schema_mismatch_rejected_before_outage(self):
        root, args, service = self.upgrade_fixture(schema=11)
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=lambda p: json.loads((p / 'manifest.json').read_text())):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, [])

    def test_same_version_reports_no_update_without_outage(self):
        root,args,service=self.upgrade_fixture(version='v1.0.0')
        before=(root/'data/state.json').read_bytes()
        with patch.object(ops,'Service',return_value=service),patch.object(ops,'check_release',side_effect=lambda p:json.loads((p/'manifest.json').read_text())):
            with self.assertRaisesRegex(ops.OpsError,'发布源没有更新版本'):
                ops.upgrade(args)
        self.assertEqual(service.calls,[])
        self.assertEqual((root/'data/state.json').read_bytes(),before)
        self.assertEqual((root / 'current').resolve().name, 'v1.0.0')

    def test_downgrade_rejected_before_outage(self):
        root, args, service = self.upgrade_fixture(version='v0.9.0')
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=lambda p: json.loads((p / 'manifest.json').read_text())):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, [])

    def test_start_failure_preserves_newest_credentials(self):
        root, args, service = self.upgrade_fixture()
        def fail_health(*args):
            (root / 'data/state.json').write_text(json.dumps({'version': 10, 'token': 'rotated-after-start'}))
            raise ops.OpsError('fixture startup failure')
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=lambda p: json.loads((p / 'manifest.json').read_text())), patch.object(ops, 'wait_health', side_effect=fail_health):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, ['stop', 'start', 'stop'])
        self.assertEqual(json.loads((root / 'data/state.json').read_text())['token'], 'rotated-after-start')
        self.assertEqual((root / 'current').resolve().name, 'v1.0.1')
        self.assertEqual(len(list((root / 'backups').glob('*.tar.gz'))), 1)
        self.assertEqual((root / 'releases/v1.0.0/codearts2api-panel').read_text(), 'old-binary')

    def test_backup_failure_keeps_old_binary_and_restarts(self):
        root, args, service = self.upgrade_fixture()
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=lambda p: json.loads((p / 'manifest.json').read_text())), patch.object(ops, 'backup', side_effect=ops.OpsError('fixture backup failure')):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, ['stop', 'start'])
        self.assertEqual((root / 'current').resolve().name, 'v1.0.0')
