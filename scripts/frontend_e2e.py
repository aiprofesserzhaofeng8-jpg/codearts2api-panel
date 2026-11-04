#!/usr/bin/env python3
"""Browser acceptance against a real binary with temporary data and fake APIs."""
import argparse
import json
import os
from pathlib import Path
import socket
import secrets
import subprocess
import tempfile
import time
import urllib.request
ROOT = Path(__file__).resolve().parents[1]
CASES = ("providers-smoke", "qoder-smoke", "kiro-smoke", "subscriptions-smoke", "quota-smoke", "trae-smoke", "trae-device-smoke", "groups-smoke", "account-create-smoke", "growth-multi-smoke", "growth-tasks-smoke", "growth-actions-smoke", "growth-buddy-smoke", "frontend-smoke", "product-settings-smoke", "users-smoke", "consumer-entitlements-smoke", "unified-console-smoke", "user-style-smoke", "i18n-smoke")
def run(binary, output):
    output.mkdir(parents=True, exist_ok=True)
    results=[]
    with tempfile.TemporaryDirectory(prefix="panel-e2e-") as folder:
        temp=Path(folder)
        with socket.socket() as sock: sock.bind(("127.0.0.1",0)); port=sock.getsockname()[1]
        with (temp/"server.log").open("w") as log:
            password=secrets.token_urlsafe(24)
            process=subprocess.Popen([str(binary),"-host","127.0.0.1","-port",str(port),"-data",str(temp/"state.json"),"-no-import","-no-refresh"],cwd=temp,env={**os.environ,"CODEARTS_ADMIN_PASSWORD":password},stdout=log,stderr=log)
            try:
                base=f"http://127.0.0.1:{port}"
                for _ in range(100):
                    try: urllib.request.urlopen(base+"/health",timeout=1).close();break
                    except OSError:
                        if process.poll() is not None: raise RuntimeError((temp/"server.log").read_text())
                        time.sleep(.1)
                else: raise RuntimeError("test binary not ready")
                env={**os.environ,"CODEARTS_TEST_BASE":base,"CODEARTS_TEST_ADMIN_PASSWORD":password,"NODE_PATH":str(ROOT/"frontend/node_modules")+os.pathsep+os.environ.get("NODE_PATH", "")}
                for name in CASES:
                    result=subprocess.run(["node",str(ROOT/"scripts"/(name+".cjs"))],cwd=ROOT,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=120)
                    (output/(name+".log")).write_text(result.stdout)
                    results.append({"case":name,"passed":result.returncode==0});print(name, "PASS" if result.returncode==0 else "FAIL",flush=True)
                    if result.returncode:print(result.stdout[-2500:])
            finally: process.terminate();process.wait(timeout=20)
    (output/"browser-results.json").write_text(json.dumps(results,indent=2)+"\n")
    if not all(result["passed"] for result in results): raise SystemExit(1)
if __name__ == "__main__":
    parser=argparse.ArgumentParser();parser.add_argument("--binary",type=Path,required=True);parser.add_argument("--output",type=Path,default=ROOT/"artifacts/frontend-e2e");args=parser.parse_args();run(args.binary.resolve(),args.output)
