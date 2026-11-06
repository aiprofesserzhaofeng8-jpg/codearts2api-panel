"""Test evidence grading: incomplete streams and HTTP 200 errors cannot pass."""
import importlib.util
import json
from pathlib import Path
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("capability_audit", Path(__file__).with_name("model-capability-audit.py"))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)


def sse(*items, done=False):
    return "".join("data: " + json.dumps(x) + "\n\n" for x in items) + ("data: [DONE]\n\n" if done else "")


class EvidenceTests(unittest.TestCase):
    def test_chat_fragments_merge_tool_arguments(self):
        raw = sse(
            {"choices": [{"delta": {"tool_calls": [{"index": 0, "id": "call_1", "function": {"name": "read_fixture", "arguments": '{"pa'}}]}}]},
            {"choices": [{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": 'th":"probe.py"}'}}]}, "finish_reason": "tool_calls"}]},
            {"choices": [], "usage": {"completion_tokens": 12}}, done=True,
        )
        result = audit.parsed("chat", raw, True)
        self.assertTrue(result["terminal"])
        self.assertEqual(json.loads(result["calls"][0]["function"]["arguments"]), {"path": "probe.py"})
        self.assertTrue(audit.positive_usage(result["usage"]))

    def test_chat_missing_done_is_not_complete(self):
        raw = sse({"choices": [{"delta": {"content": "OK"}, "finish_reason": "stop"}]})
        self.assertFalse(audit.parsed("chat", raw, True)["terminal"])

    def test_chat_reasoning_is_preserved_for_tool_history(self):
        raw = sse({"choices": [{"delta": {"reasoning_content": "Need the fixture. "}}]}, {"choices": [{"delta": {"reasoning_content": "Call read."}, "finish_reason": "tool_calls"}]}, done=True)
        result = audit.parsed("chat", raw, True)
        self.assertEqual(result["assistant"]["reasoning_content"], "Need the fixture. Call read.")

    def test_http_200_stream_error_is_failure(self):
        result, error = audit.evaluated({"status": 200, "raw": sse({"error": {"message": "upstream failed"}}, done=True)}, "chat", True)
        self.assertIsNone(result)
        self.assertIn("upstream failed", error)

    def test_empty_http_200_is_not_text_success(self):
        result, error = audit.evaluated({"status": 200, "raw": '{"choices":[],"usage":{}}'}, "chat")
        self.assertIsNone(error)
        self.assertEqual(result["text"], "")
        self.assertFalse(result["terminal"])
        self.assertFalse(audit.positive_usage(result["usage"]))

    def test_anthropic_fragmented_tool_json(self):
        raw = sse(
            {"type": "message_start", "message": {"usage": {"input_tokens": 22}}},
            {"type": "content_block_start", "index": 0, "content_block": {"type": "tool_use", "id": "c", "name": "read_fixture", "input": {}}},
            {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": '{"path":"probe.py"}'}},
            {"type": "content_block_stop", "index": 0},
            {"type": "message_delta", "delta": {"stop_reason": "tool_use"}, "usage": {"output_tokens": 11}},
            {"type": "message_stop"},
        )
        result = audit.parsed("messages", raw, True)
        self.assertEqual(result["calls"][0]["input"], {"path": "probe.py"})
        self.assertEqual(result["usage"], {"input_tokens": 22, "output_tokens": 11})
        self.assertTrue(result["terminal"])

    def test_responses_incomplete_not_normal_finish(self):
        raw = sse({"type": "response.incomplete", "response": {"status": "incomplete", "output": []}})
        result = audit.parsed("responses", raw, True)
        self.assertEqual(result["finish"], "incomplete")
        self.assertNotIn(result["finish"], ("stop", "end_turn", "completed"))

    def test_secret_scrubbing(self):
        self.assertNotIn("actual-secret", audit.scrub("actual-secret sk-ca-abc123 github_pat_xyz123", "actual-secret"))
        self.assertNotIn("sk-ca-abc123", audit.scrub("sk-ca-abc123"))

    def test_only_pre_admission_gateway_rate_limit_is_retried(self):
        class Response:
            def __init__(self, code, data):
                self.code = code
                self.data = data.encode()
                self.headers = {"Retry-After": "1", "Content-Type": "application/json"}

            def __enter__(self):
                return self

            def __exit__(self, *args):
                return False

            def read1(self, size):
                value, self.data = self.data, b""
                return value

        api = audit.API("http://127.0.0.1:1", "test-key", min_interval=0)
        with mock.patch.object(audit.urllib.request, "urlopen", side_effect=[Response(429, "该密钥已触发网关限流"), Response(200, "{}")]) as send, mock.patch.object(audit.time, "sleep"):
            result = api.request("/v1/models")
        self.assertEqual(send.call_count, 2)
        self.assertEqual(result["status"], 200)
        self.assertEqual(result["gateway_rate_limit_attempts"][0]["status"], 429)
        with mock.patch.object(audit.urllib.request, "urlopen", return_value=Response(429, "ChatGPT 上游请求失败（HTTP 429）")) as send:
            result = api.request("/v1/models")
        self.assertEqual(send.call_count, 1)
        self.assertEqual(result["status"], 429)


if __name__ == "__main__":
    unittest.main()
