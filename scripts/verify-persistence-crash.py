#!/usr/bin/env python3
"""Exercise the unmodified gateway binary with private state and a local TLS fixture.

No production state, accounts, ports, service controls, or upstream credentials are
used. The JSON distinguishes successful execution from crash durability.
"""
import argparse
import hashlib
import http.server
import json
import os
import pathlib
import secrets
import signal
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]
MODEL = "persistence-fixture-model"
HOSTS = ("snap-access.cn-north-4.myhuaweicloud.com",
         "snap-access.cn-north-4.myhuaweicloud.cn")
USAGE = {"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16,
         "prompt_tokens_details": {"cached_tokens": 3}}
LIMITS = {"requests": {"max_requests": 1}, "tokens": {"max_tokens": 16},
          "usd": {"max_usd": 0.000016}}
STATS = ("total_requests", "usage_requests", "input_tokens", "output_tokens",
         "cached_tokens", "estimated_usd", "failed_requests")


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")
    path.chmod(0o600)


class QuietHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def reply(self, code, value, content_type="application/json"):
        raw = value if isinstance(value, bytes) else json.dumps(value).encode()
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


class UpstreamHandler(QuietHandler):
    def do_GET(self):
        if self.path != "/v1/model/builtin":
            self.server.unexpected.append(self.path)
            self.reply(500, {"error": "unexpected fixture path"})
            return
        self.reply(200, {"count": 1, "builtinModels": [
            {"model_id": MODEL, "model_name": MODEL, "enable": True}]})

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path != "/api/v2/chat/completions" or body.get("model") != MODEL:
            self.server.unexpected.append(self.path)
            self.reply(500, {"error": "unexpected fixture request"})
            return
        self.server.inferences.append({"time": time.monotonic(),
                                       "stream": bool(body.get("stream"))})
        common = {"id": "chatcmpl-persistence-fixture", "model": MODEL,
                  "created": int(time.time())}
        if body.get("stream"):
            chunks = [
                {**common, "object": "chat.completion.chunk", "choices": [
                    {"index": 0, "delta": {"role": "assistant", "content": "OK"},
                     "finish_reason": None}]},
                {**common, "object": "chat.completion.chunk", "choices": [
                    {"index": 0, "delta": {}, "finish_reason": "stop"}],
                 "usage": USAGE}]
            raw = "".join("data: " + json.dumps(c) + "\n\n" for c in chunks)
            self.reply(200, (raw + "data: [DONE]\n\n").encode(), "text/event-stream")
        else:
            self.reply(200, {**common, "object": "chat.completion", "choices": [
                {"index": 0, "message": {"role": "assistant", "content": "OK"},
                 "finish_reason": "stop"}], "usage": USAGE})


class ProxyHandler(QuietHandler):
    def do_CONNECT(self):
        # Terminate TLS locally. There is deliberately no outbound forwarding.
        if self.path not in {host + ":443" for host in HOSTS}:
            self.server.unexpected.append("CONNECT " + self.path)
            self.reply(502, {"error": "fixture destination refused"})
            return
        self.send_response(200, "Connection Established")
        self.end_headers()
        self.close_connection = True
        with self.server.tls.wrap_socket(self.connection, server_side=True) as conn:
            UpstreamHandler(conn, self.client_address, self.server)


def api(port, path, data=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Authorization"] = "Bearer " + key
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}" + path,
        None if data is None else json.dumps(data).encode(), headers=headers)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(request, timeout=5) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


def snapshot(state, logs, usage_count):
    return {"stats": {k: state.get("stats", {}).get(k, 0) for k in STATS},
            "budgets": {k["name"]: k.get("budget_usage", {}) for k in state["keys"]},
            "log_count": len(logs), "usage_record_count": usage_count,
            "request_ids": sorted(x["request_id"] for x in logs)}


def disk_snapshot(path):
    state = json.loads(path.read_text())
    return snapshot(state, state.get("logs") or [], len(state.get("usage_records") or []))


def journal_snapshot(path):
    journal = path.with_name(path.name + ".usage.jsonl")
    if not journal.exists():
        return {"bytes": 0, "records": 0}
    raw = journal.read_bytes()
    return {"bytes": len(raw), "records": raw.count(b"\n")}


def live_snapshot(port):
    code, state = api(port, "/admin/api/state")
    require(code == 200, "cannot read isolated admin state")
    code, logs = api(port, "/admin/api/logs")
    require(code == 200, "cannot read isolated request logs")
    code, usage = api(port, "/admin/api/usage")
    require(code == 200, "cannot read isolated usage history")
    return snapshot(state, logs["logs"], usage["total"])


class Gateway:
    def __init__(self, binary, directory, env):
        self.binary, self.directory, self.env = binary, directory, env
        self.state = directory / "state.json"
        self.process = None
        self.keys = {name: "sk-ca-" + secrets.token_hex(24) for name in LIMITS}
        write_json(self.state, {
            "version": 4, "usage_history_initialized": True,
            "accounts": [{"id": "fixture", "name": "fixture", "source": "manual",
                          "enabled": True, "status": "ok", "session": {
                              "access_key": "fixture-ak", "secret_key": "fixture-sk",
                              "credential_kind": "aksk"}}],
            "keys": [{"name": name, "key": key, "enabled": True, "budget": LIMITS[name],
                      "budget_usage": {"requests": 0, "tokens": 0, "usd": 0,
                                       "unpriced": False, "incomplete": False}}
                     for name, key in self.keys.items()],
            "settings": {"auto_claim": False, "auto_refresh": False,
                         "write_back_to_ide": False, "rate_limit_enabled": False,
                         "max_retries": 1},
            "model_prices": {MODEL: {"currency": "USD", "input": 1, "output": 1,
                                     "cached": 1, "source": "synthetic fixture"}},
            "stats": {}, "logs": [], "usage_records": []})

    def start(self):
        # The gateway does not expose its assigned :0 port, so reserve an unused
        # loopback port briefly; readiness and catalog assertions detect races.
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            self.port = sock.getsockname()[1]
        self.log = (self.directory / "gateway.log").open("ab")
        self.started = time.monotonic()
        self.process = subprocess.Popen([
            str(self.binary), "-host", "127.0.0.1", "-port", str(self.port),
            "-data", str(self.state), "-no-import", "-no-refresh", "-local-auto-unlock"],
            env=self.env, stdout=self.log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            require(self.process.poll() is None, "isolated gateway exited during startup")
            try:
                code, result = api(self.port, "/v1/models", key=self.keys["requests"])
                if code == 200 and any(x["id"] == MODEL for x in result["data"]):
                    return
            except OSError:
                pass
            time.sleep(0.05)
        raise RuntimeError("isolated gateway or local TLS fixture did not become ready")

    def stop(self, sig):
        require(self.process is not None and self.process.poll() is None,
                "isolated gateway already stopped")
        self.process.send_signal(sig)
        result = self.process.wait(timeout=15)
        self.log.close()
        require(result == (-signal.SIGKILL if sig == signal.SIGKILL else 0),
                "isolated gateway had unexpected exit code")
        return result

    def cleanup(self):
        if self.process is not None and self.process.poll() is None:
            self.process.kill()
            self.process.wait(timeout=10)
        if hasattr(self, "log"):
            self.log.close()

    def infer(self, name):
        if name == "tokens":
            path, body = "/v1/responses", {"input": "reply OK", "max_output_tokens": 16}
        else:
            path = "/v1/messages" if name == "usd" else "/v1/chat/completions"
            body = {"messages": [{"role": "user", "content": "reply OK"}], "max_tokens": 16}
        return api(self.port, path, {**body, "model": MODEL, "stream": False}, self.keys[name])


def scenario(binary, directory, env, name):
    directory.mkdir(mode=0o700)
    gateway = Gateway(binary, directory, env)
    try:
        gateway.start()
        initial = disk_snapshot(gateway.state)
        for budget in LIMITS:
            code, _ = gateway.infer(budget)
            require(code == 200, f"{name}: first {budget} request failed with {code}")
        # Wait for handler completion, which may slightly trail client EOF.
        deadline = time.monotonic() + 5
        while True:
            live = live_snapshot(gateway.port)
            if live["stats"]["total_requests"] == 3:
                break
            require(time.monotonic() < deadline, "request accounting did not complete")
            time.sleep(0.02)
        require(live["log_count"] == 3 and live["usage_record_count"] == 3,
                "completed requests missing from live logs/history")
        for usage in live["budgets"].values():
            require(usage["requests"] == 1 and usage["tokens"] == 16
                    and abs(usage["usd"] - 0.000016) < 1e-12
                    and not usage["unpriced"] and not usage["incomplete"],
                    "fixture did not produce complete known usage and price")
        blocked = {budget: gateway.infer(budget)[0] for budget in LIMITS}
        require(all(code == 402 for code in blocked.values()), "live budget was not enforced")
        before_stop = disk_snapshot(gateway.state)
        if name == "periodic_then_kill":
            deadline = gateway.started + 40
            while disk_snapshot(gateway.state) != live:
                require(time.monotonic() < deadline, "periodic save did not finish within 40s")
                time.sleep(0.1)
            before_stop = disk_snapshot(gateway.state)
        elif name == "kill_before_flush":
            require(time.monotonic() - gateway.started < 20,
                    "quick-kill test took too long to isolate the periodic window")
        journal_before_stop = journal_snapshot(gateway.state)
        stop_after = round(time.monotonic() - gateway.started, 3)
        exit_code = gateway.stop(signal.SIGTERM if name == "graceful" else signal.SIGKILL)
        after_stop = disk_snapshot(gateway.state)
        gateway.start()
        recovered = live_snapshot(gateway.port)
        # A durable append journal may legitimately recover newer accounting
        # than the periodic JSON snapshot. The old binary has no journal.
        require(recovered in (after_stop, live), "restart recovered inconsistent accounting")
        lost_ids = sorted(set(live["request_ids"]) - set(recovered["request_ids"]))
        replay = {budget: gateway.infer(budget)[0] for budget in LIMITS}
        expected_replay = 200 if lost_ids else 402
        require(all(code == expected_replay for code in replay.values()),
                "post-restart budget behavior does not match recovered accounting")
        durable = recovered == live
        if name != "kill_before_flush":
            require(durable, f"{name}: control scenario lost completed accounting")
        gateway.stop(signal.SIGTERM)
        return {"name": name, "stop_after_start_sec": stop_after, "exit_code": exit_code,
                "initial_disk": initial, "completed_in_memory": live,
                "live_budget_rejection_status": blocked, "disk_before_stop": before_stop,
                "journal_before_stop": journal_before_stop,
                "disk_after_stop": after_stop, "recovered_before_replay": recovered,
                "lost_request_ids": lost_ids, "post_restart_status": replay,
                "completed_accounting_survived": durable}
    finally:
        gateway.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=pathlib.Path, default=ROOT / "bin/codearts2api-panel")
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--require-durable", action="store_true",
                        help="fail if any completed accounting is lost on restart")
    args = parser.parse_args()
    os.umask(0o077)
    binary = args.binary.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="codearts-persistence-") as temp:
        directory = pathlib.Path(temp)
        cert, key = directory / "fixture.crt", directory / "fixture.key"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                        "-days", "1", "-subj", "/CN=" + HOSTS[0],
                        "-addext", "subjectAltName=" + ",".join("DNS:" + h for h in HOSTS),
                        "-addext", "basicConstraints=critical,CA:TRUE",
                        "-keyout", str(key), "-out", str(cert)], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        tls.load_cert_chain(cert, key)
        proxy = http.server.ThreadingHTTPServer(("127.0.0.1", 0), ProxyHandler)
        proxy.daemon_threads = True
        proxy.tls, proxy.inferences, proxy.unexpected = tls, [], []
        thread = threading.Thread(target=proxy.serve_forever, daemon=True)
        thread.start()
        # Keep the caller's environment untouched. No real credentials are read.
        env = {k: v for k, v in os.environ.items()
               if not k.upper().startswith(("CODEARTS_", "HTTP_PROXY", "HTTPS_PROXY",
                                            "ALL_PROXY", "NO_PROXY", "SSL_CERT_"))}
        proxy_url = f"http://127.0.0.1:{proxy.server_port}"
        env.update(HTTP_PROXY=proxy_url, HTTPS_PROXY=proxy_url, NO_PROXY="127.0.0.1,localhost",
                   SSL_CERT_FILE=str(cert), SSL_CERT_DIR=str(directory))
        results = []
        try:
            for name in ("kill_before_flush", "graceful", "periodic_then_kill"):
                result = scenario(binary, directory / name, env, name)
                results.append(result)
                print(json.dumps({"scenario": name,
                                  "completed_accounting_survived": result["completed_accounting_survived"],
                                  "post_restart_status": result["post_restart_status"]}), flush=True)
            require(not proxy.unexpected, "unexpected request reached the local proxy fixture")
            expected_calls = 9 + sum(code == 200 for result in results
                                     for code in result["post_restart_status"].values())
            require(len(proxy.inferences) == expected_calls,
                    "budget rejection unexpectedly reached upstream")
            args.output.parent.mkdir(parents=True, exist_ok=True)
            write_json(args.output, {
                "execution_passed": True,
                "completed_accounting_survived_all_scenarios": all(
                    r["completed_accounting_survived"] for r in results),
                "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                "upstream": "loopback TLS fixture; CONNECT has no outbound forwarding",
                "real_provider_requests": 0, "fixture_inferences": len(proxy.inferences),
                "scenarios": results})
            if args.require_durable:
                require(all(r["completed_accounting_survived"] for r in results),
                        "completed request accounting was lost across restart")
        finally:
            proxy.shutdown()
            proxy.server_close()
            thread.join(timeout=2)


if __name__ == "__main__":
    main()
