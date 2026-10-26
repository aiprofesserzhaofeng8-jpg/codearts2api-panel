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

    def test_codex_working_native_sandbox_is_accepted(self):
        with mock.patch.object(connect.sys,'platform','linux'), mock.patch.object(connect.subprocess,'run',return_value=SimpleNamespace(returncode=0)):
            connect.check_codex_sandbox()

    def test_url_and_redirect_validation(self):
        self.assertEqual(connect.normalize_url('https://gateway.example/v1/'),'https://gateway.example')
        for url in ['file:///tmp/a','https://user:secret@example.com','https://example.com?key=secret']:
            with self.assertRaises(ValueError):connect.normalize_url(url)
        self.assertIsNone(connect.NoRedirect().redirect_request(None,None,302,'',{},'https://elsewhere.example'))

if __name__=='__main__': unittest.main()
