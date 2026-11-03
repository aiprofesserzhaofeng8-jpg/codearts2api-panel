"""Public GitHub fallback must preserve version, origin and checksum constraints."""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
import urllib.error
from unittest.mock import patch

SOURCE = Path(__file__).resolve().parents[1] / 'install.sh'
module = types.ModuleType('installer_public')
exec(SOURCE.read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0], module.__dict__)


class PublicReleaseTest(unittest.TestCase):
    def fixture(self, code=403, tamper=False):
        tag = 'v0.3.8'
        name = f'codearts2api-panel-{tag}-linux-amd64.tar.gz'
        blobs = {name: b'archive bytes', 'panelctl.py': b'helper bytes'}
        sums = ''.join(hashlib.sha256(b).hexdigest() + '  ' + n + '\n' for n, b in blobs.items()).encode()
        def fetch(url, target, limit):
            if url.startswith(module.API):
                raise urllib.error.HTTPError(url, code, 'API error', {}, io.BytesIO())
            body = sums if url.endswith('/SHA256SUMS') else blobs[url.rsplit('/', 1)[1]]
            if tamper and url.endswith('/panelctl.py'):
                body = b'tampered helper'
            target.write_bytes(body)
        return tag, name, blobs, fetch

    def test_rate_limit_downloads_and_checks_bytes_without_executing(self):
        tag, name, blobs, fetch = self.fixture()
        with tempfile.TemporaryDirectory() as folder:
            output = Path(folder) / 'download'
            argv = ['installer', 'download', '--output', str(output)]
            with patch.object(module, 'fetch', side_effect=fetch), patch.object(module, 'public_latest_version', return_value=tag), patch.object(module.platform, 'machine', return_value='x86_64'), patch.object(module.sys, 'argv', argv), patch.object(module.subprocess, 'run') as run:
                module.main()
                self.assertEqual((output / name).read_bytes(), blobs[name])
                self.assertEqual((output / 'panelctl.py').read_bytes(), blobs['panelctl.py'])
                run.assert_not_called()

    def test_tampered_fallback_helper_never_executes(self):
        tag, _, _, fetch = self.fixture(tamper=True)
        with tempfile.TemporaryDirectory() as folder, patch.object(module, 'fetch', side_effect=fetch), patch.object(module, 'public_latest_version', return_value=tag), patch.object(module.platform, 'machine', return_value='x86_64'), patch.object(module.sys, 'argv', ['installer', 'install', '--root', folder + '/new', '--non-interactive']), patch.object(module.subprocess, 'run') as run:
            with self.assertRaises(module.InstallError):
                module.main()
            run.assert_not_called()
            self.assertFalse(Path(folder, 'new').exists())

    def test_fallback_installs_only_verified_files(self):
        tag, _, _, fetch = self.fixture()
        with tempfile.TemporaryDirectory() as folder, patch.object(module, 'fetch', side_effect=fetch), patch.object(module, 'public_latest_version', return_value=tag), patch.object(module.platform, 'machine', return_value='x86_64'), patch.object(module.sys, 'argv', ['installer', 'install', '--root', folder + '/new', '--non-interactive']), patch.object(module.subprocess, 'run') as run:
            module.main()
            self.assertEqual(run.call_count, 1)
            self.assertIn('--sha256', run.call_args.args[0])

    def test_pinned_old_release_never_silently_changes_version(self):
        tag, _, _, fetch = self.fixture()
        with tempfile.TemporaryDirectory() as folder, patch.object(module, 'fetch', side_effect=fetch), patch.object(module, 'public_latest_version', return_value=tag):
            with self.assertRaises(module.InstallError):
                module.release(Path(folder), 'v0.3.7')
            self.assertFalse((Path(folder) / 'public-SHA256SUMS').exists())
            result, _ = module.release(Path(folder), tag)
            self.assertEqual(result, tag)

    def test_not_found_and_authentication_errors_do_not_fallback(self):
        for code in (400, 401, 404, 422):
            _, _, _, fetch = self.fixture(code=code)
            with self.subTest(code=code), tempfile.TemporaryDirectory() as folder, patch.object(module, 'fetch', side_effect=fetch), patch.object(module, 'public_release') as fallback:
                with self.assertRaises(urllib.error.HTTPError):
                    module.release(Path(folder), 'latest')
                fallback.assert_not_called()

    def test_unavailable_api_uses_public_fallback(self):
        for error in (TimeoutError(), urllib.error.URLError('connection failed'), urllib.error.HTTPError(module.API, 429, 'limited', {}, io.BytesIO()), urllib.error.HTTPError(module.API, 503, 'unavailable', {}, io.BytesIO())):
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as folder, patch.object(module, 'fetch', side_effect=error), patch.object(module, 'public_release', return_value=('v0.3.8', {})) as fallback:
                self.assertEqual(module.release(Path(folder), 'latest')[0], 'v0.3.8')
                fallback.assert_called_once()

    def test_invalid_api_metadata_does_not_fallback(self):
        for content in ('not json', json.dumps({'tag_name': 'v0.3.8', 'draft': True}), json.dumps({'tag_name': 'v0.3.8', 'prerelease': True})):
            with tempfile.TemporaryDirectory() as folder, patch.object(module, 'fetch', side_effect=lambda url, path, limit: path.write_text(content)), patch.object(module, 'public_release') as fallback:
                with self.assertRaises((ValueError, module.InstallError)):
                    module.release(Path(folder), 'latest')
                fallback.assert_not_called()

    def test_public_redirect_is_exact_repo_stable_tag_and_never_authenticated(self):
        good = 'https://github.com/' + module.REPO + '/releases/tag/v0.3.8'
        for location, valid in ((good, True), ('/' + module.REPO + '/releases/tag/v0.3.8', True),
                                (good + '-rc1', False), (good + '?x=y', False), (good + '#fragment', False),
                                (good.replace(module.REPO, 'attacker/repo'), False),
                                ('https://evil.invalid/v0.3.8', False), (good.replace('https:', 'http:'), False)):
            with self.subTest(location=location), patch.dict(os.environ, {'GITHUB_TOKEN': 'test-only-token'}), patch.object(module.urllib.request, 'build_opener') as build:
                build.return_value.open.side_effect = urllib.error.HTTPError('latest', 302, 'Found', {'Location': location}, io.BytesIO())
                if valid:
                    self.assertEqual(module.public_latest_version(), 'v0.3.8')
                else:
                    with self.assertRaises(module.InstallError):
                        module.public_latest_version()
                request = build.return_value.open.call_args.args[0]
                self.assertEqual(request.get_method(), 'HEAD')
                self.assertIsNone(request.get_header('Authorization'))
                self.assertEqual(request.full_url, 'https://github.com/' + module.REPO + '/releases/latest')

    def test_missing_or_malicious_public_manifest_is_rejected(self):
        for body in ('garbage', '0' * 64 + '  ../panelctl.py\n', ('0' * 64 + '  panelctl.py\n') * 2):
            with self.subTest(body=body), tempfile.TemporaryDirectory() as folder, patch.object(module, 'public_latest_version', return_value='v0.3.8'), patch.object(module, 'fetch', side_effect=lambda url, path, limit: path.write_text(body)):
                with self.assertRaises(module.InstallError):
                    module.public_release(Path(folder), 'latest')
