import unittest

from check_architecture import PREFIX, decode_packages, violations


class ArchitectureRulesTest(unittest.TestCase):
    def check_edge(self, source, target, rejected):
        result = violations([{"ImportPath": PREFIX + source, "Imports": [target]}])
        self.assertEqual(bool(result), rejected, (source, target, result))

    def test_business_cannot_reintroduce_http_store_or_platform(self):
        for source in ("accounts", "config", "gateway", "jobs", "catalog", "catalog/routing", "upstream"):
            for target in ("store", "server", "app", "workbuddy", "upstream/codearts"):
                self.check_edge(source, PREFIX + target, True)
            self.check_edge(source, "net/http", True)
        self.check_edge("accounts", PREFIX + "gateway", True)
        self.check_edge("protocol", PREFIX + "accounts", True)
        self.check_edge("protocol/search", PREFIX + "accounts", True)

    def test_protocol_can_reuse_only_pure_conversion_library(self):
        self.check_edge("protocol", PREFIX + "responsescompat", False)
        self.check_edge("responsescompat", PREFIX + "protocol", True)
        result = violations([
            {"ImportPath": PREFIX + "protocol", "Imports": [PREFIX + "responsescompat"]},
            {"ImportPath": PREFIX + "responsescompat", "Imports": ["net/http"]},
        ])
        self.assertTrue(any("indirect" in error for error in result))

    def test_platform_isolation_and_intentional_assembly(self):
        for source in ("codearts", "workbuddy", "trae", "upstream/codearts"):
            for target in ("codearts", "workbuddy", "trae", "upstream/codearts"):
                self.check_edge(source, PREFIX + target, source.split("/")[-1] != target.split("/")[-1])
        for source, target in (("app", "server"), ("app", "storage"), ("app", "upstream/codearts"), ("server", "accounts"), ("accounts", "provider"), ("registry", "codearts"), ("apierr", "codearts"), ("store", "storage")):
            self.check_edge(source, PREFIX + target, False)
        self.check_edge("upstream/codearts", "net/http", False)

    def test_new_runtime_owners_and_facade_direction(self):
        self.check_edge("storage", "net/http", True)
        self.check_edge("store", PREFIX + "gateway", True)
        for source in ("accounts", "gateway", "usage", "scheduler"):
            self.check_edge(source, PREFIX + "storage", True)
        for source in ("app", "server", "storage", "registry"):
            self.check_edge(source, PREFIX + "store", True)
        for target in ("accounts", "gateway", "storage"):
            self.check_edge("scheduler", PREFIX + target, True)
        self.check_edge("scheduler", "net/http", True)

    def test_indirect_legacy_bridge_cannot_hide_platform_or_http(self):
        packages = [
            {"ImportPath": PREFIX + "catalog", "Imports": [PREFIX + "apierr"]},
            {"ImportPath": PREFIX + "apierr", "Imports": [PREFIX + "codearts", "net/http"]},
            {"ImportPath": PREFIX + "codearts", "Imports": ["net/http"]},
        ]
        result = violations(packages)
        self.assertTrue(any("catalog -> " + PREFIX + "apierr -> " + PREFIX + "codearts" in e for e in result))
        self.assertTrue(any("apierr -> net/http" in e for e in result))
        # A platform bridge outside the neutral business graph remains explicit.
        self.assertEqual(violations(packages[1:]), [])

    def test_go_json_stream_is_not_a_json_array(self):
        packages = [{"ImportPath": PREFIX + "protocol", "Imports": ["encoding/json"]}, {"ImportPath": PREFIX + "accounts", "Imports": [PREFIX + "store"]}]
        import json
        self.assertEqual(decode_packages("\n" + "\n\n".join(map(json.dumps, packages)) + "\n"), packages)
        self.assertEqual(len(violations(packages)), 1)
