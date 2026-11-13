"""Verify binary packages retain the reused component notices and hashes."""
import importlib.util
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

DEPLOY = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(DEPLOY))
spec = importlib.util.spec_from_file_location('linux_package_licenses', DEPLOY / 'package_release.py')
packaging = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packaging)


class PackageLicenseTests(unittest.TestCase):
    def setUp(self):
        source = patch.object(packaging, 'prepare_source', return_value={'commit': 'fixture', 'dirty': False, 'channel': 'stable'})
        source.start()
        self.addCleanup(source.stop)

    def test_linux_archive_contains_licenses_and_source_revision(self):
        def fake_build(command, **kwargs):
            Path(command[command.index('-o') + 1]).write_bytes(b'isolated-package-fixture')
        with tempfile.TemporaryDirectory() as temp, patch.object(packaging.subprocess, 'run', side_effect=fake_build):
            archive = packaging.build('v0.0.0', 'amd64', Path(temp), 'go')
            with tarfile.open(archive, 'r:gz') as package:
                manifest = json.load(package.extractfile('manifest.json'))
                self.assertEqual(manifest['source']['commit'], 'fixture')
                for name in ('NOTICE', 'LICENSE.sub2api', 'COPYING.sub2api', 'SUB2API-METADATA.md', 'SUB2API-METADATA.json', 'SUB2API-PROTOCOL.md'):
                    self.assertIn(name, manifest['files'])
                    data = package.extractfile('data/' + name).read()
                    self.assertEqual(__import__('hashlib').sha256(data).hexdigest(), manifest['files'][name]['sha256'])
                source = json.load(package.extractfile('data/SUB2API-METADATA.json'))
                self.assertEqual(source['revision'], '6b4b37db43443b5e88e424fe3c69602cc3810dfb')
