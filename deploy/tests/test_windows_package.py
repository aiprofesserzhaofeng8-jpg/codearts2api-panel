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


class WindowsPackageTest(unittest.TestCase):
    def fake_build(self, command, **kwargs):
        Path(command[command.index('-o') + 1]).write_bytes(b'MZ-test-exe')

    def test_complete_archive_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as temp, patch.object(packaging.subprocess, 'run', side_effect=self.fake_build):
            output = Path(temp)
            archive = packaging.build('v0.3.7', output, 'go')
            with zipfile.ZipFile(archive) as package:
                manifest = json.loads(package.read('manifest.json'))
                self.assertEqual(set(package.namelist()), set(manifest['files']) | {'manifest.json'})
                self.assertEqual(manifest['os'], 'windows')
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
