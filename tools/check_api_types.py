#!/usr/bin/env python3
"""Fail if the committed TypeScript contract differs from the schema generator."""
from pathlib import Path
import subprocess
import tempfile
ROOT = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix="panel-api-types-") as folder:
    output = Path(folder) / "admin.ts"
    subprocess.run([str(ROOT / "scripts/frontend.sh"), "exec", "openapi-typescript", str(ROOT / "api/admin.openapi.yaml"), "-o", str(output)], check=True)
    if output.read_bytes() != (ROOT / "frontend/src/api/generated/admin.ts").read_bytes():
        raise SystemExit("Generated API types differ; run ./scripts/frontend.sh generate")
print("Generated API contract is current")
