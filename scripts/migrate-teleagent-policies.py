"""Save the five TeleAgent presets in the local panel before upgrading."""
import getpass
import http.cookiejar
import json
import sys
import urllib.error
import urllib.request
from pathlib import Path

BASE = "http://127.0.0.1:10087"
POLICIES = "/admin/api/model-policies"


def main():
    presets = json.loads((Path(__file__).resolve().parents[1] /
                          "deploy/teleagent-model-policies.json").read_text())
    client = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(path, data=None):
        req = urllib.request.Request(BASE + path,
                                     data=json.dumps(data).encode() if data is not None else None,
                                     headers={"Origin": BASE, "Content-Type": "application/json"})
        with client.open(req, timeout=15) as response:
            return json.load(response)

    username = input("管理员登录名 [admin]: ").strip() or "admin"
    request("/auth/login", {"username": username, "password": getpass.getpass("管理员密码: ")})
    state = request(POLICIES)
    revision = state["routing_revision"]
    for model, policy in presets.items():
        limits = state["client_compatibility"].get(model, {}).get("limits", [])
        if len(limits) != 1 or not limits[0].get("basis"):
            raise ValueError(f"{model} 的目录或策略来源不明确，请在模型页检查")
        if limits[0]["basis"] == "administrator_config":
            print(f"保留已有策略 {model}")
            continue
        print(f"正在保存 {model}", flush=True)
        result = request(POLICIES, {"model": model, "policy": policy,
                                    "expected_routing_revision": revision})
        if not result["ok"] or result["policies"][model]["context_window"] != policy["context_window"] or result["policies"][model]["max_output_tokens"] != policy["max_output_tokens"]:
            raise ValueError(f"{model} 保存回执与预期不符")
        revision = result["routing_revision"]
        print(f"已保存 {model}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, urllib.error.URLError) as error:
        sys.exit(f"迁移中止：{error}")
