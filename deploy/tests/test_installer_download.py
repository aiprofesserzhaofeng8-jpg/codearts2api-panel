import io
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import patch
import urllib.error

SOURCE = Path(__file__).resolve().parents[1] / 'install.sh'
module = types.ModuleType('installer_download')
exec(SOURCE.read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0], module.__dict__)
ASSET = module.BASE + 'v0.3.8/codearts2api-panel-v0.3.8-linux-amd64.tar.gz'


class Response(io.BytesIO):
    def __init__(self, body, length=None):
        super().__init__(body)
        self.headers = {'Content-Length': str(len(body) if length is None else length)}


class DownloadTest(unittest.TestCase):
    def error(self, code):
        return urllib.error.HTTPError(ASSET, code, 'failed', {}, io.BytesIO())

    def test_server_error_retries_original_asset_with_fresh_query(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(module.urllib.request, 'build_opener') as build, patch.object(module.time, 'sleep') as sleep, patch.dict(os.environ, {'GITHUB_TOKEN': 'test-token'}):
            build.return_value.open.side_effect = [self.error(500), Response(b'complete')]
            target = Path(folder) / 'archive'
            module.fetch(ASSET, target, 100)
            self.assertEqual(target.read_bytes(), b'complete')
            requests = [call.args[0] for call in build.return_value.open.call_args_list]
            self.assertEqual(requests[0].full_url, ASSET)
            self.assertTrue(requests[1].full_url.startswith(ASSET + '?codearts_retry='))
            self.assertTrue(all(r.get_header('Authorization') is None for r in requests))
            self.assertEqual(requests[1].get_header('Cache-control'), 'no-cache')
            sleep.assert_called_once_with(1)

    def test_expired_signed_url_and_interrupted_download_are_retried(self):
        for first in (self.error(618), TimeoutError(), ConnectionResetError(), Response(b'partial', length=100)):
            with self.subTest(first=type(first).__name__), tempfile.TemporaryDirectory() as folder, patch.object(module.urllib.request, 'build_opener') as build, patch.object(module.time, 'sleep'):
                build.return_value.open.side_effect = [first, Response(b'complete')]
                target = Path(folder) / 'archive'
                module.fetch(ASSET, target, 200)
                self.assertEqual(target.read_bytes(), b'complete')
                self.assertEqual(build.return_value.open.call_count, 2)

    def test_retries_bounded_and_partial_file_removed(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(module.urllib.request, 'build_opener') as build, patch.object(module.time, 'sleep') as sleep:
            build.return_value.open.side_effect = [Response(b'incomplete', length=30) for _ in range(3)]
            target = Path(folder) / 'archive'
            with self.assertRaises(module.http.client.IncompleteRead):
                module.fetch(ASSET, target, 100)
            self.assertFalse(target.exists())
            self.assertEqual(build.return_value.open.call_count, 3)
            self.assertEqual([c.args for c in sleep.call_args_list], [(1,), (2,)])

    def test_not_found_auth_errors_and_api_limits_not_retried(self):
        for url, code in ((ASSET, 404), (ASSET, 403), (ASSET, 401), (module.API + 'latest', 429)):
            with self.subTest(url=url, code=code), tempfile.TemporaryDirectory() as folder, patch.object(module.urllib.request, 'build_opener') as build, patch.object(module.time, 'sleep') as sleep:
                build.return_value.open.side_effect = self.error(code)
                with self.assertRaises(urllib.error.HTTPError):
                    module.fetch(url, Path(folder) / 'archive', 100)
                self.assertEqual(build.return_value.open.call_count, 1)
                sleep.assert_not_called()

    def test_size_limit_and_invalid_origin_never_retried(self):
        for response in (Response(b'oversized'), module.InstallError('untrusted redirect')):
            with tempfile.TemporaryDirectory() as folder, patch.object(module.urllib.request, 'build_opener') as build, patch.object(module.time, 'sleep') as sleep:
                build.return_value.open.side_effect = [response]
                target = Path(folder) / 'archive'
                with self.assertRaises(module.InstallError):
                    module.fetch(ASSET, target, 1)
                self.assertFalse(target.exists())
                self.assertEqual(build.return_value.open.call_count, 1)
                sleep.assert_not_called()

    def test_existing_target_is_never_removed_or_overwritten(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(module.urllib.request, 'build_opener') as build:
            target = Path(folder) / 'archive'
            target.write_bytes(b'existing')
            with self.assertRaises(module.InstallError):
                module.fetch(ASSET, target, 100)
            self.assertEqual(target.read_bytes(), b'existing')
            build.assert_not_called()

    def test_http_diagnostics_distinguish_status_without_signed_query(self):
        with patch.object(module, 'LANGUAGE', 'zh'):
            error = urllib.error.HTTPError(ASSET + '?sig=private-value', 500, 'failed', {}, io.BytesIO())
            output = module.http_failure(error)
            self.assertIn('HTTP 500', output)
            self.assertIn('github.com/codearts2api-panel', output)
            self.assertIn('暂时异常', output)
            self.assertNotIn('sig=', output)
            self.assertNotIn('private-value', output)
            self.assertNotIn('版本号', output)
            self.assertIn('未找到', module.http_failure(self.error(404)))
