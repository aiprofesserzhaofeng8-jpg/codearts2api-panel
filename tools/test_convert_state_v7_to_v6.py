import unittest
from convert_state_v7_to_v6 import convert

class ConversionTest(unittest.TestCase):
    def fixture(self):
        return {'version':7,'accounts':[{'session':{'refresh_token':'latest-generation'}}], 'usage_sequence':23,'groups':[{'id':'g'}],'settings':{'common_prompt':{'prompt_mode':'custom','prompt_text':'COMMON','prompt_fallback':True,'sanitize':True},'codearts':{'prompt_mode':'passthrough'},'workbuddy':{'prompt_overrides':{'prompt_text':'','prompt_fallback':False,'sanitize':False}},'trae':{'prompt_overrides':{}}}}
    def test_current_credentials_history_and_false_empty_preserved(self):
        state=self.fixture();out=convert(state)
        self.assertEqual(out['version'],6);self.assertEqual(out['accounts'],state['accounts']);self.assertEqual(out['usage_sequence'],23)
        self.assertEqual(out['settings']['workbuddy'],{'prompt_mode':'custom','prompt_text':'','prompt_fallback':False,'sanitize':False})
        self.assertEqual(out['settings']['trae']['prompt_text'],'COMMON');self.assertEqual(out['settings']['codearts'],{'prompt_mode':'passthrough'});self.assertEqual(state['version'],7)
    def test_fail_closed_on_unrepresentable_permissions_and_defaults(self):
        for key,value in [('model_rules',[{'id':'x','callable':False}]),('default_model','x'),('client_default_model','x')]:
            state=self.fixture();state['groups'][0][key]=value
            with self.assertRaises(ValueError):convert(state)
        state=self.fixture();state['version']=99
        with self.assertRaises(ValueError):convert(state)
