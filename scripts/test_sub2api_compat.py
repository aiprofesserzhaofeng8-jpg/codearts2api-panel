"""Reviewed adaptations must not disable upstream/local source integrity checks."""
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('compat_check', Path(__file__).with_name('check-sub2api-compat.py'))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class SourceIntegrityTest(unittest.TestCase):
    def test_original_and_reviewed_adaptation_reject_further_drift(self):
        with tempfile.TemporaryDirectory() as temp:
            source = Path(temp)
            path = source / 'bridge.go'
            path.write_bytes(b'original')
            original = hashlib.sha256(path.read_bytes()).hexdigest()
            manifest = {'source_files': {'bridge.go': original}}
            check.verify_sources(source, manifest)
            path.write_bytes(b'reviewed adaptation')
            with self.assertRaises(RuntimeError):
                check.verify_sources(source, manifest)
            manifest['adapted_source_files'] = {'bridge.go': {
                'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                'reason': 'preserve metadata',
            }}
            check.verify_sources(source, manifest)
            self.assertEqual(manifest['source_files']['bridge.go'], original)
            path.write_bytes(b'unreviewed change')
            with self.assertRaises(RuntimeError):
                check.verify_sources(source, manifest)

    def test_adaptations_require_origin_and_reason(self):
        for adaptations in ({'other.go': {'sha256': 'x', 'reason': 'x'}},
                            {'bridge.go': {'sha256': 'x'}}):
            with self.assertRaises(RuntimeError):
                check.verify_sources(Path('/not-used'), {
                    'source_files': {'bridge.go': 'original'},
                    'adapted_source_files': adaptations,
                })


if __name__ == '__main__':
    unittest.main()
