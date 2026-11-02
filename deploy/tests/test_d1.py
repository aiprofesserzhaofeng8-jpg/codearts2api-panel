"""D1 failure cases: complete snapshots, atomic publication, manager state, redaction."""
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
import test_panelctl as base_tests


class D1Test(unittest.TestCase):
    setUp = base_tests.OperationsTest.setUp
    make_archive = base_tests.OperationsTest.make_archive

    def test_unreadable_child_never_silently_omitted(self):
        child = self.data / 'restricted'
        child.mkdir()
        (child / 'important.json').write_text('synthetic-private-data')
        original = os.scandir
        def inaccessible(path):
            if Path(path) == child:
                raise PermissionError('synthetic-private-path')
            return original(path)
        with patch.object(ops.os, 'scandir', side_effect=inaccessible):
            with self.assertRaises(PermissionError):
                ops.backup(self.data, self.archive)
            args = ops.parser().parse_args(['doctor', '--data-dir', str(self.data)])
            with patch.object(ops, 'health', return_value='running'), patch('sys.stdout', new_callable=io.StringIO) as out:
                self.assertEqual(ops.doctor(args), 1)
                self.assertNotIn('synthetic-private', out.getvalue())
        self.assertFalse(self.archive.exists())

    def test_restore_race_cannot_replace_an_empty_directory(self):
        ops.backup(self.data, self.archive)
        target = self.root / 'restored'
        original = ops.publish_directory
        inode = []
        def competing_directory(source, destination):
            destination.mkdir(mode=0o700)
            inode.append(destination.stat().st_ino)
            original(source, destination)
        with patch.object(ops, 'publish_directory', side_effect=competing_directory):
            with self.assertRaises(ops.OpsError):
                ops.unpack(self.archive, target, 'backup')
        self.assertEqual(target.stat().st_ino, inode[0])
        self.assertEqual(list(target.iterdir()), [])
        self.assertFalse(list(self.root.glob('.restore-*')))

    def test_restore_race_cannot_replace_symlink(self):
        ops.backup(self.data, self.archive)
        target, current = self.root / 'restored', self.root / 'current'
        current.mkdir()
        (current / 'token').write_text('synthetic-new-token')
        original = ops.publish_directory
        def competing_link(source, destination):
            destination.symlink_to(current, target_is_directory=True)
            original(source, destination)
        with patch.object(ops, 'publish_directory', side_effect=competing_link):
            with self.assertRaises(ops.OpsError):
                ops.unpack(self.archive, target, 'backup')
        self.assertTrue(target.is_symlink())
        self.assertEqual((current / 'token').read_text(), 'synthetic-new-token')

    def test_restore_checks_schema_and_state_presence(self):
        cases = [({'state_name': 'state.json', 'schema': 9}, {'state.json': b'{"version":10}'}),
                 ({'state_name': 'state.json', 'schema': 10}, {'unrelated': b'x'}),
                 ({'state_name': '../state.json', 'schema': 10}, {'state.json': b'{"version":10}'}),
                 ({'state_name': 'state.json', 'schema': True}, {'state.json': b'{"version":1}'})]
        for meta, files in cases:
            with self.subTest(meta=meta):
                archive = self.make_archive(files, **meta)
                with self.assertRaises(ops.OpsError):
                    ops.unpack(archive, self.root / 'restored', 'backup')
                self.assertFalse((self.root / 'restored').exists())

    def test_missing_main_state_not_misdiagnosed_as_fresh_setup(self):
        (self.data / 'state.json').unlink()
        args = ops.parser().parse_args(['doctor', '--data-dir', str(self.data)])
        with patch.object(ops, 'health', return_value='setup_required'), patch('sys.stdout', new_callable=io.StringIO) as out:
            self.assertEqual(ops.doctor(args), 1)
            self.assertIn('主状态缺失', out.getvalue())
        with self.assertRaises(ops.OpsError):
            ops.backup(self.data, self.archive)

    def test_doctor_checks_nested_directory_permissions(self):
        for file in self.data.iterdir():
            file.chmod(0o600)
        child = self.data / 'nested'
        child.mkdir(mode=0o755)
        args = ops.parser().parse_args(['doctor', '--data-dir', str(self.data)])
        with patch.object(ops, 'health', return_value='running'), patch('sys.stdout', new_callable=io.StringIO) as out:
            self.assertEqual(ops.doctor(args), 1)
            self.assertIn('子目录', out.getvalue())
        child.chmod(0o700)
        with patch.object(ops, 'health', return_value='running'), patch('sys.stdout', new_callable=io.StringIO):
            self.assertEqual(ops.doctor(args), 0)

    def test_restore_syncs_nested_directories_before_publication(self):
        child = self.data / 'one/two'
        child.mkdir(parents=True)
        (child / 'checkpoint.json').write_text('synthetic-checkpoint')
        ops.backup(self.data, self.archive)
        events = []
        original_sync, original_publish = ops.sync_dir, ops.publish_directory
        def sync(path):
            events.append(('sync', str(path)))
            original_sync(path)
        def publish(source, target):
            events.append(('publish', str(source)))
            original_publish(source, target)
        with patch.object(ops, 'sync_dir', side_effect=sync), patch.object(ops, 'publish_directory', side_effect=publish):
            ops.unpack(self.archive, self.root / 'restored', 'backup')
        publish_index = next(i for i, event in enumerate(events) if event[0] == 'publish')
        self.assertTrue(any(path.endswith('/one/two') for kind, path in events[:publish_index] if kind == 'sync'))

    def test_restart_failure_returns_nonzero_but_keeps_valid_archive(self):
        class FakeService:
            def running(self): return True
            def stop(self): pass
            def start(self): raise ops.OpsError('synthetic restart failure')
        with self.assertRaises(ops.OpsError):
            with ops.stopped(self.data, FakeService()):
                ops.backup(self.data, self.archive)
        ops.unpack(self.archive, self.root / 'restored', 'backup')
        self.assertEqual(ops.inventory(self.data), ops.inventory(self.root / 'restored'))

    def test_failure_after_writing_partial_archive_leaves_no_output(self):
        original = ops.tarfile.TarFile.add
        count = 0
        def disk_error(tar, *args, **kwargs):
            nonlocal count
            count += 1
            if count == 2:
                raise OSError('synthetic disk failure')
            return original(tar, *args, **kwargs)
        before = ops.inventory(self.data)
        with patch.object(ops.tarfile.TarFile, 'add', disk_error), self.assertRaises(OSError):
            ops.backup(self.data, self.archive)
        self.assertFalse(self.archive.exists())
        self.assertFalse(list(self.root.glob('.backup-*')))
        self.assertEqual(ops.inventory(self.data), before)

    def test_compose_rejects_restarting_paused_unknown_and_missing(self):
        service = ops.Service(compose=str(self.root / 'compose.yml'))
        for state in ('restarting', 'paused', 'removing', 'unknown'):
            result = subprocess.CompletedProcess([], 0, stdout=json.dumps([{'State': state}]))
            with patch.object(ops, 'run', return_value=result), self.assertRaises(ops.OpsError):
                service.running()
        for empty in ('', '[]'):
            result = subprocess.CompletedProcess([], 0, stdout=empty)
            with patch.object(ops, 'run', return_value=result), self.assertRaises(ops.OpsError):
                service.running()

    def test_compose_supports_array_and_json_lines(self):
        service = ops.Service(compose=str(self.root / 'compose.yml'))
        for raw, active in [('[{"State":"running"}]', True), ('{"State":"exited"}', False),
                            ('{"State":"running"}\n{"State":"exited"}', True)]:
            result = subprocess.CompletedProcess([], 0, stdout=raw)
            with patch.object(ops, 'run', return_value=result):
                self.assertEqual(service.running(), active)

    def test_offline_declaration_is_required(self):
        with self.assertRaises(ops.OpsError):
            with ops.stopped(self.data):
                self.fail('entered without offline declaration')
