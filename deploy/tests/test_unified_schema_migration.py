"""Model-schema upgrade boundaries using isolated fixtures, never real services."""
import json
from pathlib import Path
import types
import unittest
from unittest.mock import patch

import test_panelctl as base
import panelctl as ops


class UnifiedSchemaUpgradeTest(unittest.TestCase):
    setUp = base.OperationsTest.setUp
    make_archive = base.OperationsTest.make_archive
    upgrade_fixture = base.UpgradeTest.upgrade_fixture

    def fixture(self):
        root, args, service = self.upgrade_fixture(schema=11)
        args.migrate_schema = True
        return root, args, service

    @staticmethod
    def metadata(path):
        meta = json.loads((path / 'manifest.json').read_text())
        if meta['schema'] == 11:
            meta['state_migrations'] = [{'from': 10, 'to': 11}]
        return meta

    @staticmethod
    def convert(binary, source, target):
        target.mkdir(mode=0o700)
        original = json.loads((source / 'state.json').read_text())
        (target / 'state.json').write_text(json.dumps({**original, 'version': 11}))

    def test_conflict_does_not_stop_service(self):
        root, args, service = self.fixture()
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=self.metadata), patch.object(ops, 'model_migration_precheck', side_effect=ops.OpsError('conflict')):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, [])
        self.assertEqual((root / 'current').resolve().name, 'v1.0.0')

    def test_migration_rechecks_after_stop_and_preserves_original(self):
        root, args, service = self.fixture()
        before = (root / 'data/state.json').read_bytes()
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=self.metadata), patch.object(ops, 'model_migration_precheck', return_value={}) as precheck, patch.object(ops, 'model_migration_copy', side_effect=self.convert), patch.object(ops, 'wait_health'):
            ops.upgrade(args)
        self.assertEqual(precheck.call_count, 2)
        self.assertEqual(service.calls, ['stop', 'start'])
        self.assertEqual(ops.state_schema(root / 'data/state.json'), 11)
        original = next(root.glob('data-before-schema11-*'))
        self.assertEqual((original / 'state.json').read_bytes(), before)
        self.assertFalse((root / 'schema-migration.json').exists())
        self.assertEqual(len(list(root.glob('schema-migration-complete-*.json'))), 1)

    def test_failed_post_start_never_restores_old_credentials(self):
        root, args, service = self.fixture()
        def fail(*_):
            (root / 'data/state.json').write_text('{"version":11,"token":"new-generation"}')
            raise ops.OpsError('health failed')
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=self.metadata), patch.object(ops, 'model_migration_precheck', return_value={}), patch.object(ops, 'model_migration_copy', side_effect=self.convert), patch.object(ops, 'wait_health', side_effect=fail):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, ['stop', 'start', 'stop'])
        self.assertIn('new-generation', (root / 'data/state.json').read_text())
        self.assertTrue((root / 'schema-migration.json').exists())
        with self.assertRaises(ops.OpsError):
            ops.upgrade(args)

    def test_second_precheck_conflict_restarts_original(self):
        root, args, service = self.fixture()
        with patch.object(ops, 'Service', return_value=service), patch.object(ops, 'check_release', side_effect=self.metadata), patch.object(ops, 'model_migration_precheck', side_effect=[{}, ops.OpsError('changed')]):
            with self.assertRaises(ops.OpsError):
                ops.upgrade(args)
        self.assertEqual(service.calls, ['stop', 'start'])
        self.assertEqual((root / 'current').resolve().name, 'v1.0.0')
        self.assertFalse((root / 'schema-migration.json').exists())

    def test_precheck_does_not_echo_secret_output(self):
        for result in [types.SimpleNamespace(returncode=1, stdout='private-key', stderr='private-key'), types.SimpleNamespace(returncode=0, stdout='private-key', stderr='')]:
            with patch.object(ops.subprocess, 'run', return_value=result):
                with self.assertRaises(ops.OpsError) as error:
                    ops.model_migration_precheck(Path('/fixture/panel'), Path('/fixture/state'))
            self.assertNotIn('private-key', str(error.exception))


if __name__ == '__main__':
    unittest.main()
