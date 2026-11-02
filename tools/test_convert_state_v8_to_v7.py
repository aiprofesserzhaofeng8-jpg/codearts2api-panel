import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('convert8', Path(__file__).with_name('convert_state_v8_to_v7.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class CompatibilityExport(unittest.TestCase):
    def test_preserves_latest_credentials_budget_and_checkpoint(self):
        state = {'version': 8, 'users': [], 'accounts': [{'credentials': 'latest-rotated-generation'}],
                 'keys': [{'budget_usage': {'requests': 12}}], 'usage_sequence': 19,
                 'logs': [], 'usage_records': [], 'settings': {'common_prompt': {'sanitize': False}}}
        original = copy.deepcopy(state)
        converted = module.convert(state, b'{"sequence":20,"entry":{"model":"m"}}\n')
        expected = copy.deepcopy(state)
        expected['version'] = 7
        expected.pop('users')
        self.assertEqual(converted, expected)
        self.assertEqual(state, original)

    def test_refuses_every_unrepresentable_owner_and_future_version(self):
        cases = [{'version': 8, 'users': [{'id': 'u'}]},
                 {'version': 8, 'keys': [{'user_id': 'u'}]},
                 {'version': 8, 'logs': [{'user_id': 'u'}]},
                 {'version': 8, 'usage_records': [{'user_id': 'u'}]}, {'version': 9}]
        for state in cases:
            with self.subTest(state=state), self.assertRaises(ValueError):
                module.convert(state)
        with self.assertRaises(ValueError):
            module.convert({'version': 8}, b'{"sequence":1,"entry":{"user_id":"u"}}\n')


if __name__ == '__main__':
    unittest.main()
