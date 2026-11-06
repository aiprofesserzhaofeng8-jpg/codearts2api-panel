import importlib.util
import json
import tempfile
import unittest
from unittest import mock
from types import SimpleNamespace
from pathlib import Path

spec = importlib.util.spec_from_file_location('connect', Path(__file__).with_name('connect-client.py'))
connect = importlib.util.module_from_spec(spec)
spec.loader.exec_module(connect)

class ClientSetupTests(unittest.TestCase):
    def setUp(self):
        self.profile = {'model':'deepseek-v4.1-flash','context_window':65536,'max_output_tokens':8192,
                        'compaction':{'reserve_tokens':16384,'keep_recent_tokens':12000,'trigger_tokens':45000}}

    def test_pi_preserves_declared_image_input(self):
        with tempfile.TemporaryDirectory() as tmp:
            profile = {**self.profile, 'input_modalities': ['text', 'image']}
            connect.build_launch('pi', 'http://127.0.0.1:10087', profile, Path(tmp), {'CODEARTS_API_KEY':'fixture'})
            data = json.loads((Path(tmp)/'pi/models.json').read_text())
            self.assertEqual(data['providers']['codearts']['models'][0]['input'], ['text', 'image'])

    def test_pi_config_contains_no_literal_key(self):
        with tempfile.TemporaryDirectory() as tmp:
            env={'CODEARTS_API_KEY':'private-canary'}
            cmd=connect.build_launch('pi','http://127.0.0.1:10087',self.profile,Path(tmp),env)
            raw=(Path(tmp)/'pi/models.json').read_text()
            self.assertNotIn('private-canary',raw)
            self.assertIn('${CODEARTS_API_KEY}',raw)
            adapter=Path(tmp)/'pi/extensions/codearts-compaction.ts'
            self.assertTrue(adapter.exists())
            self.assertIn('session_before_compact',adapter.read_text())
            self.assertNotIn('private-canary',adapter.read_text())
            self.assertEqual(cmd[-1],'deepseek-v4.1-flash')
            settings=json.loads((Path(tmp)/'pi/settings.json').read_text())
            self.assertEqual(settings['compaction']['modelOverrides']['codearts/deepseek-v4.1-flash']['keepRecentTokens'],12000)

    def test_pi_bundled_adapter_matches_reviewed_source(self):
        source=Path(__file__).parent/'pi/codearts-compaction.ts'
        self.assertEqual(connect.PI_COMPACTION_ADAPTER,source.read_text())

    def test_pi_disabled_summary_removes_managed_adapter(self):
        with tempfile.TemporaryDirectory() as tmp:
            env={'CODEARTS_API_KEY':'private-canary'}
            connect.build_launch('pi','https://gateway.example',self.profile,Path(tmp),env)
            self.profile['pi_summary_adapter']=False
            connect.build_launch('pi','https://gateway.example',self.profile,Path(tmp),env)
            self.assertFalse((Path(tmp)/'pi/extensions/codearts-compaction.ts').exists())

    def test_codex_selects_responses_without_credentials_in_args(self):
        cmd=connect.build_launch('codex','https://gateway.example',self.profile,Path('/tmp'),{'CODEARTS_API_KEY':'private-canary'})
        self.assertNotIn('private-canary',' '.join(cmd))
        self.assertIn('model_providers.codearts_gateway.wire_api="responses"',cmd)
        self.assertIn('model_providers.codearts_gateway.supports_websockets=false',cmd)

    def test_claude_isolates_existing_provider_config(self):
        env={'CODEARTS_API_KEY':'private-canary','ANTHROPIC_API_KEY':'old-key','CLAUDE_CODE_USE_OPENAI':'1','OPENAI_API_KEY':'other-key'}
        cmd=connect.build_launch('claude','http://127.0.0.1:10087',self.profile,Path('/tmp'),env)
        self.assertNotIn('CLAUDE_CODE_USE_OPENAI',env)
        self.assertNotIn('ANTHROPIC_API_KEY',env)
        self.assertEqual(env['ANTHROPIC_BASE_URL'],'http://127.0.0.1:10087')
        self.assertEqual(env['CLAUDE_CODE_MAX_CONTEXT_TOKENS'],'65536')
        self.assertEqual(cmd[:3],['claude','--setting-sources',''])
        self.assertNotIn('private-canary',' '.join(cmd))

    def test_codex_failed_sandbox_stops_before_launch(self):
        with mock.patch.object(connect.sys,'platform','linux'), mock.patch.object(connect.subprocess,'run',return_value=SimpleNamespace(returncode=1)) as runner:
            with self.assertRaisesRegex(ValueError,'no model request was sent'):
                connect.check_codex_sandbox()
            self.assertIn('sandbox',runner.call_args.args[0])
            self.assertNotIn('danger-full-access',str(runner.call_args))

    def test_claude_automatically_omits_unsupported_controls(self):
        for model in ['deepseek-v4.1-flash', 'workbuddy/cn/deepseek-v4.1-flash', 'trae/cn/deepseek-v4.1-flash']:
            profile = {**self.profile, 'model': model}
            env = {'CODEARTS_API_KEY': 'private-canary', 'CLAUDE_CODE_EFFORT_LEVEL': 'high', 'MAX_THINKING_TOKENS': '4096'}
            connect.build_launch('claude', 'http://127.0.0.1:10087', profile, Path('/tmp'), env)
            self.assertEqual(env['CLAUDE_CODE_EFFORT_LEVEL'], 'auto')
            self.assertEqual(env['MAX_THINKING_TOKENS'], '0')
        profile = {**self.profile, 'claude_code': {'thinking_control': True, 'effort_control': True}}
        env = {'CODEARTS_API_KEY': 'private-canary', 'CLAUDE_CODE_EFFORT_LEVEL': 'medium', 'MAX_THINKING_TOKENS': '2048'}
        connect.build_launch('claude', 'http://127.0.0.1:10087', profile, Path('/tmp'), env)
        self.assertEqual(env['CLAUDE_CODE_EFFORT_LEVEL'], 'medium')
        self.assertEqual(env['MAX_THINKING_TOKENS'], '2048')

    def test_codex_working_native_sandbox_is_accepted(self):
        with mock.patch.object(connect.sys,'platform','linux'), mock.patch.object(connect.subprocess,'run',return_value=SimpleNamespace(returncode=0)):
            connect.check_codex_sandbox()

    def test_url_and_redirect_validation(self):
        self.assertEqual(connect.normalize_url('https://gateway.example/v1/'),'https://gateway.example')
        for url in ['file:///tmp/a','https://user:secret@example.com','https://example.com?key=secret']:
            with self.assertRaises(ValueError):connect.normalize_url(url)
        self.assertIsNone(connect.NoRedirect().redirect_request(None,None,302,'',{},'https://elsewhere.example'))

if __name__=='__main__': unittest.main()

class PiSyncTests(unittest.TestCase):
    def test_sync_updates_images_preserves_limits_secrets_other_providers_and_backup(self):
        with tempfile.TemporaryDirectory() as folder:
            path=Path(folder)/'models.json'
            original={'providers':{'other':{'apiKey':'other-canary'},'codearts-local':{'baseUrl':'http://127.0.0.1:10087/v1','apiKey':'local-canary','models':[{'id':'shared-v1','name':'old','input':['text'],'contextWindow':32768,'maxTokens':4096}]}}}
            path.write_text(json.dumps(original));before=path.read_bytes()
            rows={'data':[{'id':'shared-v1','provider':'unified','input_modalities':['text','image'],'client_profile':None},{'id':'unknown-v1','client_profile':None}]}
            response=mock.MagicMock();response.__enter__.return_value.read.return_value=json.dumps(rows).encode()
            with mock.patch.object(connect.urllib.request,'build_opener') as opener:
                opener.return_value.open.return_value=response
                result=connect.sync_pi_models('http://127.0.0.1:10087',path)
            saved=json.loads(path.read_text());model=saved['providers']['codearts-local']['models'][0]
            self.assertEqual(model['input'],['text','image']);self.assertEqual(model['contextWindow'],32768)
            self.assertEqual(saved['providers']['other'],original['providers']['other'])
            self.assertEqual(saved['providers']['codearts-local']['apiKey'],'local-canary')
            self.assertEqual(Path(result['backup']).read_bytes(),before)
            self.assertEqual(result['skipped_missing_limits'],['unknown-v1'])
            self.assertNotIn('canary',json.dumps(result))
    def test_sync_rejects_different_gateway_before_sending_credentials(self):
        with tempfile.TemporaryDirectory() as folder:
            path=Path(folder)/'models.json';path.write_text(json.dumps({'providers':{'codearts-local':{'baseUrl':'http://127.0.0.1:10087/v1','apiKey':'secret'}}}))
            with self.assertRaises(ValueError):connect.sync_pi_models('https://other.example',path)
