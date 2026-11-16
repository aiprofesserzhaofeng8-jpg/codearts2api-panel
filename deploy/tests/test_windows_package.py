import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location('windows_package', Path(__file__).parents[1] / 'package_windows.py')
packaging = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packaging)


acceptance_spec = importlib.util.spec_from_file_location('windows_acceptance', Path(__file__).with_name('windows_acceptance.py'))
acceptance = importlib.util.module_from_spec(acceptance_spec)
acceptance_spec.loader.exec_module(acceptance)


class WindowsPackageTest(unittest.TestCase):
    def setUp(self):
        source = patch.object(packaging, 'prepare_source', return_value={'commit': 'fixture', 'dirty': False, 'channel': 'stable'})
        source.start()
        self.addCleanup(source.stop)

    def fake_build(self, command, **kwargs):
        Path(command[command.index('-o') + 1]).write_bytes(b'MZ-test-exe')

    def test_complete_archive_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as temp, patch.object(packaging.subprocess, 'run', side_effect=self.fake_build):
            output = Path(temp)
            archive = packaging.build('v0.3.7', output, 'go')
            with zipfile.ZipFile(archive) as package:
                manifest = acceptance.check_package(package)
                self.assertEqual(set(package.namelist()), set(manifest['files']) | {'manifest.json'})
                self.assertEqual(manifest['os'], 'windows')
                self.assertEqual(manifest['source']['commit'], 'fixture')
                for name in ('NOTICE', 'LICENSE.sub2api', 'COPYING.sub2api', 'SUB2API-METADATA.md', 'SUB2API-METADATA.json', 'SUB2API-PROTOCOL.md', 'SUB2API-CNAPI.md', 'SUB2API-CNAPI.json', 'LICENSE.cnapi', 'COPYING.cnapi', 'SUB2API-GEMINI.md', 'SUB2API-GEMINI.json', 'LICENSE.gemini', 'COPYING.gemini', 'SUB2API-ANTIGRAVITY.md', 'SUB2API-ANTIGRAVITY.json', 'LICENSE.antigravity', 'COPYING.antigravity', 'SUB2API-GROK.md', 'SUB2API-GROK.json', 'LICENSE.grok', 'COPYING.grok', 'SUB2API-OPENCODE.md', 'SUB2API-OPENCODE.json', 'LICENSE.opencode', 'COPYING.opencode', 'SUB2API-COMMANDCODE.md', 'SUB2API-COMMANDCODE.json', 'LICENSE.commandcode', 'COPYING.commandcode', 'SUB2API-CLINE.md', 'SUB2API-CLINE.json', 'LICENSE.cline', 'COPYING.cline', 'SUB2API-VERTEX.md', 'SUB2API-VERTEX.json', 'LICENSE.vertex', 'COPYING.vertex', 'SUB2API-BEDROCK.md', 'SUB2API-BEDROCK.json', 'LICENSE.bedrock', 'COPYING.bedrock', 'LICENSE.aws-sdk-go-v2', 'NOTICE.aws-sdk-go-v2', 'LICENSE.smithy-go', 'NOTICE.smithy-go', 'SUB2API-TYPESAFE.md', 'SUB2API-TYPESAFE.json', 'LICENSE.typesafe', 'COPYING.typesafe', 'SUB2API-GOOGLEWIRE.md', 'SUB2API-GOOGLEWIRE.json', 'LICENSE.googlewire', 'COPYING.googlewire'):
                    self.assertIn(name, manifest['files'])
                for name, info in manifest['files'].items():
                    self.assertEqual(packaging.hashlib.sha256(package.read(name)).hexdigest(), info['sha256'])
                self.assertIn(b'\r\n', package.read('start.cmd'))
                self.assertNotIn(b'\r\r\n', package.read('start.cmd'))
            original = archive.read_bytes()
            with self.assertRaises(FileExistsError):
                packaging.build('v0.3.7', output, 'go')
            self.assertEqual(archive.read_bytes(), original)
            self.assertEqual(Path(str(archive) + '.sha256').read_text().split()[0], packaging.sha256(archive))

    def test_zip_failure_never_publishes_partial_archive(self):
        with tempfile.TemporaryDirectory() as temp, patch.object(packaging.subprocess, 'run', side_effect=self.fake_build), \
                patch.object(packaging.zipfile.ZipFile, 'write', side_effect=OSError('disk full')):
            with self.assertRaises(OSError):
                packaging.build('v0.3.7', Path(temp), 'go')
            self.assertEqual(list(Path(temp).iterdir()), [])

    def test_rejects_invalid_version_before_build(self):
        with tempfile.TemporaryDirectory() as temp, patch.object(packaging.subprocess, 'run') as command:
            with self.assertRaises(ValueError):
                packaging.build('../invalid', Path(temp), 'go')
            command.assert_not_called()


if __name__ == '__main__':
    unittest.main()
