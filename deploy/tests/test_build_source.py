import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from build_source import prepare_source
from panelctl import release_order, release_version


class BuildSourceTests(unittest.TestCase):
    def test_local_to_stable_order(self):
        self.assertLess(release_order('v0.4.11-local.20261112'), release_order('v0.4.11'))
        self.assertLess(release_order('v0.4.10'), release_order('v0.4.11-local.20261112'))
        self.assertEqual(release_version('v0.4.11'), (0, 4, 11))
        for version in ('v0.4.11-local.bad', '../local.20261112', 'v0.4.11-local.'):
            with self.assertRaises(Exception):
                release_version(version)

    def test_dirty_stable_rejected_and_local_records_source(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'source.go').write_text('fixture')
            with patch('build_source.subprocess.check_output', side_effect=[b'abc', b' M source.go']), patch('build_source.subprocess.check_call') as build:
                with self.assertRaises(RuntimeError):
                    prepare_source(root, 'v0.4.11')
                build.assert_not_called()
            with patch('build_source.subprocess.check_output', side_effect=[b'abc', b' M source.go', b'source.go\0']), patch('build_source.subprocess.check_call') as build:
                meta = prepare_source(root, 'v0.4.11-local.20261112')
                self.assertTrue(meta['dirty'])
                self.assertEqual(meta['channel'], 'local')
                self.assertEqual(len(meta['sha256']), 64)
                build.assert_called_once()
