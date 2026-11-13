"""Release publication and upgrade failure boundaries; synthetic state only."""
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import panelctl as ops
import package_release as packaging
import test_panelctl as base_tests


class UpgradeBoundaryTest(unittest.TestCase):
    setUp = base_tests.OperationsTest.setUp
    make_archive = base_tests.OperationsTest.make_archive
    upgrade_fixture = base_tests.UpgradeTest.upgrade_fixture

    def contexts(self, service):
        from contextlib import ExitStack
        stack = ExitStack()
        stack.enter_context(patch.object(ops, 'Service', return_value=service))
        stack.enter_context(patch.object(ops, 'check_release', side_effect=lambda p: json.loads((p / 'manifest.json').read_text())))
        return stack

    def test_mismatched_running_binary_rejected_before_stop(self):
        root, args, service = self.upgrade_fixture()
        with self.contexts(service), patch.object(ops, 'verify_process', side_effect=ops.OpsError('wrong running binary')):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, [])
        self.assertEqual((root / 'current').resolve().name, 'v1.0.0')

    def test_leftover_switch_rejected_before_outage(self):
        root, args, service = self.upgrade_fixture()
        (root / 'current.next').symlink_to('releases/missing')
        with self.contexts(service), patch.object(ops, 'verify_process'):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, [])
        self.assertTrue((root / 'current.next').is_symlink())

    def test_sync_failure_after_switch_does_not_restart_candidate(self):
        root, args, service = self.upgrade_fixture()
        original_sync = ops.sync_dir
        def fail_after_switch(path):
            if Path(path) == root and (root / 'current').resolve().name == 'v1.0.1':
                raise OSError('synthetic directory sync failure')
            original_sync(path)
        with self.contexts(service), patch.object(ops, 'verify_process'), patch.object(ops, 'sync_dir', side_effect=fail_after_switch):
            with self.assertRaises(OSError):
                ops.upgrade(args)
        self.assertEqual(service.calls, ['stop', 'stop'])
        self.assertEqual((root / 'current').resolve().name, 'v1.0.1')
        self.assertEqual(json.loads((root / 'data/state.json').read_text())['token'], 'current')
        self.assertEqual(len(list((root / 'backups').glob('*.tar.gz'))), 1)

    def test_stop_command_failure_attempts_to_restore_original_service(self):
        root, args, service = self.upgrade_fixture()
        original = service.stop
        def partial_stop():
            original()
            raise ops.OpsError('synthetic manager failure after stopping')
        service.stop = partial_stop
        with self.contexts(service), patch.object(ops, 'verify_process'):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, ['stop', 'start'])
        self.assertEqual((root / 'current').resolve().name, 'v1.0.0')


class PackageBoundaryTest(unittest.TestCase):
    def setUp(self):
        base_tests.OperationsTest.setUp(self)
        source = patch.object(packaging, 'prepare_source', return_value={'commit': 'fixture', 'dirty': False, 'channel': 'stable'})
        source.start()
        self.addCleanup(source.stop)

    def fake_build(self, command, **kwargs):
        Path(command[command.index('-o') + 1]).write_bytes(b'synthetic-release-binary')
        return subprocess.CompletedProcess(command, 0)

    def test_tar_write_failure_leaves_no_published_package(self):
        output = self.root / 'packages'
        with patch.object(packaging.subprocess, 'run', side_effect=self.fake_build), \
             patch.object(packaging.tarfile.TarFile, 'add', side_effect=OSError('synthetic disk failure')):
            with self.assertRaises(OSError):
                packaging.build('v1.2.3', 'amd64', output, 'synthetic-go')
        self.assertEqual(list(output.iterdir()), [])

    def test_existing_digest_never_overwritten(self):
        output = self.root / 'packages'
        output.mkdir()
        digest = output / 'codearts2api-panel-v1.2.3-linux-amd64.tar.gz.sha256'
        digest.write_text('existing-digest')
        with patch.object(packaging.subprocess, 'run', side_effect=self.fake_build):
            with self.assertRaises((ops.OpsError, RuntimeError)):
                packaging.build('v1.2.3', 'amd64', output, 'synthetic-go')
        self.assertEqual(digest.read_text(), 'existing-digest')
        self.assertEqual(list(output.iterdir()), [digest])

    def test_package_contains_current_templates_and_valid_digest(self):
        output = self.root / 'packages'
        with patch.object(packaging.subprocess, 'run', side_effect=self.fake_build):
            archive = packaging.build('v1.2.3', 'amd64', output, 'synthetic-go')
        target = self.root / 'unpacked'
        ops.unpack(archive, target, 'release', ops.sha256(archive))
        for name in ('panelctl.py', 'Caddyfile', 'compose.production.yml', 'healthcheck.sh', '.env.example', 'README.md'):
            self.assertEqual((target / name).read_bytes(), (packaging.ROOT / 'deploy' / name).read_bytes())
        for name in ('LICENSE', 'LICENSE.workbuddy', 'LICENSE.qoder', 'LICENSE.kiro', 'LICENSE.teleagent'):
            self.assertEqual((target / name).read_bytes(), (packaging.ROOT / name).read_bytes())
        self.assertEqual(archive.with_name(archive.name + '.sha256').read_text(), f'{ops.sha256(archive)}  {archive.name}\n')
        self.assertEqual(json.loads((target / 'manifest.json').read_text())['arch'], 'amd64')

    def test_invalid_programmatic_arch_refused_before_build(self):
        with patch.object(packaging.subprocess, 'run', side_effect=self.fake_build) as build:
            with self.assertRaises((ops.OpsError, RuntimeError)):
                packaging.build('v1.2.3', 'unsupported', self.root / 'packages', 'synthetic-go')
            build.assert_not_called()
