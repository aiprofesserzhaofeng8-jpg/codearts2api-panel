#!/usr/bin/env python3
"""Run the Vite proxy and an isolated local backend; stop both process groups."""
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
ROOT = Path(__file__).resolve().parents[1]
processes = []

def stop(*_):
    for process in reversed(processes):
        if process.poll() is None:
            if os.name == "posix": os.killpg(process.pid, signal.SIGTERM)
            else: process.terminate()
    for process in reversed(processes):
        try: process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            if os.name == "posix": os.killpg(process.pid, signal.SIGKILL)
            else: process.kill()

if __name__ == "__main__":
    for event in (signal.SIGINT, signal.SIGTERM): signal.signal(event, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    with tempfile.TemporaryDirectory(prefix="panel-dev-") as folder:
        try:
            subprocess.run([str(ROOT / "scripts/frontend.sh"), "install", "--frozen-lockfile"], check=True)
            subprocess.run([str(ROOT / "scripts/frontend.sh"), "build"], check=True)
            go = os.environ.get("CODEARTS_GO_BIN", os.environ.get("GO", "go"))
            processes.append(subprocess.Popen([go, "run", "./cmd/panel", "-host", "127.0.0.1", "-port", "10089", "-data", str(Path(folder)/"state.json"), "-no-import", "-no-refresh"], cwd=ROOT/"backend", start_new_session=os.name=="posix"))
            processes.append(subprocess.Popen([str(ROOT/"scripts/frontend.sh"), "dev"], cwd=ROOT, start_new_session=os.name=="posix"))
            print("Dev UI: http://127.0.0.1:5173 ; isolated data:", folder, flush=True)
            while all(process.poll() is None for process in processes): time.sleep(.2)
            raise SystemExit(next((p.returncode for p in processes if p.returncode),0))
        except KeyboardInterrupt: pass
        finally: stop()
