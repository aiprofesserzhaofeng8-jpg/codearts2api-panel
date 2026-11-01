#!/usr/bin/env python3
"""Check production import boundaries using the module's selected Go toolchain.

Legacy registry/apierr/store/native bridges remain visible and are not certified
as migrated. Rules apply to dependencies, never file counts or source line counts.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys

PREFIX = "codearts2api/internal/"
NEUTRAL = {
    "access", "accounts", "catalog", "config", "gateway", "jobs", "protocol",
    "provider", "ratelimit", "requestpolicy", "responsescompat", "upstream", "usage",
}
PLATFORMS = {"codearts", "workbuddy", "trae"}
PURE = {"access", "protocol", "ratelimit", "responsescompat", "usage"}


def violations(packages):
    errors = []
    for pkg in packages:
        name = pkg["ImportPath"]
        if not name.startswith(PREFIX):
            continue
        relative = name.removeprefix(PREFIX)
        for dep in pkg.get("Imports", []):
            local = dep.removeprefix(PREFIX) if dep.startswith(PREFIX) else None
            top = local.split("/", 1)[0] if local else None
            reason = None
            if relative in NEUTRAL:
                if top in {"server", "httpapi", "app", "store", "storage", *PLATFORMS} or (local and local.startswith("upstream/")):
                    reason = "neutral business module must use an injected interface"
                elif dep == "net/http" or dep.startswith("net/http/"):
                    reason = "HTTP belongs to transport or platform adapters"
                elif top == "gateway" and relative != "gateway":
                    reason = "business module must not depend back on gateway"
            if relative in PURE and local:
                reason = "value/algorithm module must remain independent of other internal modules"
            platform = relative.split("/")[-1] if relative.startswith("upstream/") else relative
            if platform in PLATFORMS and (top in PLATFORMS or (local and local.startswith("upstream/"))):
                target = local.split("/")[-1]
                if target in PLATFORMS and target != platform:
                    reason = "platforms must not import each other"
            if relative != "app" and top in {"app", "server", "httpapi"} and relative not in {"server", "httpapi"}:
                reason = "lower modules must not import application or HTTP assembly"
            if reason:
                errors.append(f"{name} -> {dep}: {reason}")
    return errors


def decode_packages(text):
    decoder, values = json.JSONDecoder(), []
    while text.strip():
        value, end = decoder.raw_decode(text.lstrip())
        values.append(value)
        text = text.lstrip()[end:]
    return values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go", help="Go executable (module toolchain is selected in backend)")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    result = subprocess.run([args.go, "list", "-json", "./internal/..."], cwd=root / "backend", text=True, capture_output=True)
    if result.returncode:
        sys.stderr.write(result.stderr)
        return result.returncode
    errors = violations(decode_packages(result.stdout))
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print("Production import boundaries passed (legacy bridges remain documented).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
