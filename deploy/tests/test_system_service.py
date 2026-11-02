import json
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch

import panelctl as ops
from test_installer import module as installer
import test_panelctl


class SystemServiceTest(unittest.TestCase):
    def test_system_manager_and_unit_identity(self):
        self.assertEqual(ops.Service(unit='codearts2api.service', system=True).prefix, ['systemctl'])
        self.assertEqual(ops.Service(unit='codearts2api.service').prefix, ['systemctl', '--user'])
        unit = ops.unit_text(Path('/opt/codearts2api'), 10087, system_user='codearts2api')
        for text in ('User=codearts2api\n', 'Group=codearts2api\n', 'NoNewPrivileges=true',
                     'ReadWritePaths=/opt/codearts2api/data', 'WantedBy=multi-user.target', '-host 127.0.0.1'):
            self.assertIn(text, unit)
        self.assertNotIn('User=root', unit)
        with self.assertRaises(ops.OpsError):
            ops.unit_text(Path('/opt/codearts2api'), 10087, system_user='bad\nUser=root')

    def test_root_and_writable_parent_rejected(self):
        with patch.object(ops.os, 'geteuid', return_value=1000):
            with self.assertRaisesRegex(ops.OpsError, 'root'):
                ops.system_root(Path('/opt/codearts2api'))
        with tempfile.TemporaryDirectory() as temp, patch.object(ops.os, 'geteuid', return_value=0):
            with self.assertRaisesRegex(ops.OpsError, 'root|不可写'):
                ops.system_root(Path(temp) / 'system-install')

    def test_existing_foreign_user_is_not_repurposed(self):
        foreign = types.SimpleNamespace(pw_uid=1000, pw_gid=1000, pw_dir='/home/someone', pw_shell='/bin/bash')
        with patch.object(ops.pwd, 'getpwnam', return_value=foreign), patch.object(ops.shutil, 'which', return_value='/usr/sbin/useradd'), patch.object(Path, 'is_file', return_value=True), patch.object(ops, 'run') as run:
            with self.assertRaisesRegex(ops.OpsError, '专用服务用户'):
                ops.service_account('codearts2api', Path('/opt/codearts2api'))
            run.assert_not_called()

    def test_no_arguments_nonroot_rejected_before_network(self):
        with patch.object(installer.sys, 'argv', ['installer']), patch.object(installer.os, 'geteuid', return_value=1000), patch.object(installer, 'fetch') as fetch:
            with self.assertRaisesRegex(installer.InstallError, 'sudo'):
                installer.main()
            fetch.assert_not_called()

    def test_no_arguments_select_system_defaults(self):
        # Stop at metadata retrieval: default resolution must happen before any download.
        with patch.object(installer.sys, 'argv', ['installer']), patch.object(installer.os, 'geteuid', return_value=0), patch.object(Path, 'is_dir', return_value=True), patch.object(Path, 'exists', return_value=False), patch.object(installer.shutil, 'which', return_value='/bin/systemctl'), patch.object(installer, 'release', side_effect=RuntimeError('preflight complete')) as release:
            with self.assertRaisesRegex(RuntimeError, 'preflight complete'):
                installer.main()
            release.assert_called_once()


class SystemUpgradeTest(unittest.TestCase):
    setUp = test_panelctl.OperationsTest.setUp
    make_archive = test_panelctl.OperationsTest.make_archive
    upgrade_fixture = test_panelctl.UpgradeTest.upgrade_fixture
    def test_scope_drives_upgrade_without_user_manager(self):
        root, args, service = self.upgrade_fixture()
        (root / 'installation.json').write_text(json.dumps({'unit': 'codearts2api.service', 'port': 10087, 'scope': 'system'}))
        with patch.object(ops, 'system_root') as preflight, patch.object(ops, 'service_account', return_value=types.SimpleNamespace(pw_gid=999)), patch.object(ops, 'system_release') as permissions, patch.object(ops, 'Service', return_value=service) as factory, patch.object(ops, 'no_writer'), patch.object(ops, 'wait_health'), patch.object(ops, 'check_release', side_effect=lambda p: json.loads((p / 'manifest.json').read_text())):
            ops.upgrade(args)
            preflight.assert_called_once_with(root)
            factory.assert_called_once_with(unit='codearts2api.service', system=True)
            permissions.assert_called_once()
        self.assertEqual(service.calls, ['stop', 'start'])
        self.assertEqual(len(list((root / 'backups').glob('*.tar.gz'))), 1)
