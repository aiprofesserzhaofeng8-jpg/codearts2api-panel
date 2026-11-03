#!/usr/bin/env python3
"""Real first-install process/browser acceptance, using disposable state only."""
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def run(binary, output):
    output.mkdir(parents=True, exist_ok=True)
    checks = []
    with tempfile.TemporaryDirectory(prefix="panel-setup-e2e-") as folder:
        temp = Path(folder)
        state = temp / "state.json"
        token_path = Path(str(state) + ".setup.json")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        base = f"http://127.0.0.1:{port}"
        env = {k: v for k, v in os.environ.items() if k != "CODEARTS_ADMIN_PASSWORD"}
        command = [str(binary), "-host", "127.0.0.1", "-port", str(port), "-data", str(state), "-no-import", "-no-refresh"]
        log = (temp / "server.log").open("w")
        process = None

        def start():
            return subprocess.Popen(command, cwd=temp, env=env, stdout=log, stderr=log)

        def stop():
            if process is not None and process.poll() is None:
                process.terminate()
                process.wait(timeout=20)

        def status():
            for _ in range(150):
                try:
                    with urllib.request.urlopen(base + "/setup/status", timeout=1) as response:
                        return json.load(response)
                except OSError:
                    if process.poll() is not None:
                        raise RuntimeError("setup process stopped; inspect private test log")
                    time.sleep(.1)
            raise RuntimeError("setup status unavailable")

        try:
            process = start()
            assert status() == {"needs_setup": True, "ready": False}
            assert json.loads(token_path.read_text()) == {"pending": True}
            email = "owner@example.com"
            stop()
            assert not state.exists(), "waiting installer persisted normal state"
            process = start()
            assert status()["needs_setup"]
            assert json.loads(token_path.read_text()) == {"pending": True}
            checks.append("interrupted_pending_install_resumes_without_token")
            password = secrets.token_urlsafe(24)
            browser_env = {**env, "CODEARTS_TEST_BASE": base,
                           "CODEARTS_TEST_ADMIN_EMAIL": email, "CODEARTS_TEST_ADMIN_PASSWORD": password,
                           "CODEARTS_SETUP_ARTIFACTS": str(output / "browser"),
                           "NODE_PATH": str(ROOT / "frontend/node_modules") + os.pathsep + env.get("NODE_PATH", "")}
            result = subprocess.run(["node", str(ROOT / "scripts/setup-smoke.cjs")], cwd=ROOT, env=browser_env,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
            (output / "browser.log").write_text(result.stdout)
            if result.returncode:
                raise RuntimeError("setup browser acceptance failed; inspect private browser.log")
            checks.append("browser_install_login_admin_create_consumer_login_and_role_isolation")
            assert not token_path.exists()
            assert Path(str(state) + ".installed").exists()
            assert not Path(str(state) + ".access.json").exists()
            raw = state.read_text()
            assert password not in raw
            assert password not in (temp / "server.log").read_text()
            stop()
            # Removing only the marker cannot reopen an installed state.
            Path(str(state) + ".installed").unlink()
            # Model a crash after state commit but before pending-marker removal/sealing.
            token_path.write_text(json.dumps({"pending": True}))
            token_path.chmod(0o600)
            process = start()
            assert status() == {"needs_setup": False, "ready": True}
            request = urllib.request.Request(base + "/auth/login", data=json.dumps({"username": email, "password": password}).encode(), headers={"Content-Type": "application/json"})
            with urllib.request.urlopen(request) as response:
                assert json.load(response)["user"]["role"] == "admin"
            assert not token_path.exists() and Path(str(state) + ".installed").exists()
            checks.append("restart_and_marker_removal_preserve_password_and_disable_setup")
            stop()
            # Restore the snapshot after this destructive test in its disposable
            # directory only. Never use this scenario against production data.
            saved_state = temp / "saved-state.json"
            shutil.move(state, saved_state)
            failed = subprocess.run(command, cwd=temp, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=10)
            assert failed.returncode != 0 and not state.exists() and not token_path.exists()
            checks.append("missing_snapshot_with_install_marker_refuses_reinitialization")
            checks.append("wizard_password_only_hashed_no_plaintext_recovery_or_secret_logs")
        finally:
            stop()
            log.close()
    report = {"passed": True, "checks": checks, "real_http_storage_and_authentication": True, "real_provider_requests": 0}
    (output / "process-results.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/initial-setup")
    args = parser.parse_args()
    run(args.binary.resolve(), args.output.resolve())
