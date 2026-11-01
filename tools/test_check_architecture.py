import unittest

from check_architecture import PREFIX, decode_packages, violations


class ArchitectureRulesTest(unittest.TestCase):
    def check_edge(self, source, target, rejected):
        result = violations([{"ImportPath": PREFIX + source, "Imports": [target]}])
        self.assertEqual(bool(result), rejected, (source, target, result))

    def test_business_cannot_reintroduce_http_store_or_platform(self):
        for source in ("accounts", "config", "gateway", "jobs", "catalog", "upstream"):
            for target in ("store", "server", "app", "workbuddy", "upstream/codearts"):
                self.check_edge(source, PREFIX + target, True)
            self.check_edge(source, "net/http", True)
        self.check_edge("accounts", PREFIX + "gateway", True)
        self.check_edge("protocol", PREFIX + "accounts", True)

    def test_platform_isolation_and_intentional_assembly(self):
        for source in ("codearts", "workbuddy", "trae", "upstream/codearts"):
            for target in ("codearts", "workbuddy", "trae", "upstream/codearts"):
                self.check_edge(source, PREFIX + target, source.split("/")[-1] != target.split("/")[-1])
        for source, target in (("app", "server"), ("app", "store"), ("app", "upstream/codearts"), ("server", "accounts"), ("accounts", "provider"), ("registry", "codearts"), ("apierr", "codearts"), ("store", "codearts")):
            self.check_edge(source, PREFIX + target, False)
        self.check_edge("upstream/codearts", "net/http", False)

    def test_go_json_stream_is_not_a_json_array(self):
        packages = [{"ImportPath": PREFIX + "protocol", "Imports": ["encoding/json"]}, {"ImportPath": PREFIX + "accounts", "Imports": [PREFIX + "store"]}]
        import json
        self.assertEqual(decode_packages("\n" + "\n\n".join(map(json.dumps, packages)) + "\n"), packages)
        self.assertEqual(len(violations(packages)), 1)
