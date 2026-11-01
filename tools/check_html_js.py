#!/usr/bin/env python3
"""Check generated production resources; frontend tests check source DOM contracts."""
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT / "backend/internal/webui/dist"

def main():
    if len(sys.argv) > 1:
        subprocess.run(["node", "--check", sys.argv[1]], check=True)
        return 0
    html = (DIST / "index.html").read_text()
    meta = json.loads((DIST / "build.json").read_text())
    assert re.fullmatch(r"[a-f0-9]{20}", meta["id"])
    assert meta["id"] in html
    assert not re.search(r'<(?:script|style)\b[^>]*>\s*[^<\s]|\s(?:on\w+|style)=', html)
    manifest = json.loads((DIST / ".vite/manifest.json").read_text())
    resources = re.findall(r'(?:src|href)="([^"]+)"', html)
    assert any(name.endswith(".js") and name.startswith("/assets/") for name in resources)
    assert any(name.endswith(".css") for name in resources)
    for name in resources:
        assert name.startswith("/") and not name.startswith("//")
        file = DIST / name.lstrip("/")
        assert file.is_file() and file.stat().st_size > 0, name
        if file.suffix == ".js":
            subprocess.run(["node", "--check", str(file)], check=True)
        if file.suffix == ".css":
            css = file.read_text()
            assert not re.search(r"@import\b|url\(", css), "CSS must remain offline/same-origin"
    for entry in manifest.values():
        assert (DIST / entry["file"]).is_file()
        for name in entry.get("css", []): assert (DIST / name).is_file()
    print("Production frontend: manifest, build ID, resource integrity, syntax and same-origin CSP checks passed")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
