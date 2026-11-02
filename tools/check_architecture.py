#!/usr/bin/env python3
"""Check direct and transitive production import boundaries with the pinned Go toolchain.

Legacy registry/apierr/native bridges remain visible. The old store facade is
one-way and forbidden to production consumers. Rules check actual dependencies.
"""
import argparse
from collections import deque
import json
from pathlib import Path
import subprocess
import sys

PREFIX = "codearts2api/internal/"
NEUTRAL = {
    "access", "accounts", "catalog", "config", "gateway", "jobs", "protocol",
    "provider", "scheduler", "ratelimit", "requestpolicy", "responsescompat", "upstream", "usage",
}
PLATFORMS = {"codearts", "workbuddy", "trae"}
PURE = {"access", "protocol", "ratelimit", "responsescompat", "scheduler", "usage"}


def neutral(relative):
    top = relative.split("/", 1)[0]
    return top in NEUTRAL and (top != "upstream" or relative == "upstream")


def violations(packages):
    errors = []
    for pkg in packages:
        name = pkg["ImportPath"]
        if not name.startswith(PREFIX):
            continue
        relative = name.removeprefix(PREFIX)
        owner = relative.split("/", 1)[0]
        for dep in pkg.get("Imports", []):
            local = dep.removeprefix(PREFIX) if dep.startswith(PREFIX) else None
            top = local.split("/", 1)[0] if local else None
            reason = None
            if neutral(relative):
                if top in {"server", "httpapi", "app", "store", "storage", *PLATFORMS} or (local and local.startswith("upstream/")):
                    reason = "neutral business module must use an injected interface"
                elif dep == "net/http" or dep.startswith("net/http/"):
                    reason = "HTTP belongs to transport or platform adapters"
                elif top == "gateway" and owner != "gateway":
                    reason = "business module must not depend back on gateway"
            if owner in PURE and local:
                reason = "value/algorithm module must remain independent of other internal modules"
            platform = relative.split("/")[-1] if relative.startswith("upstream/") else relative
            if platform in PLATFORMS and (top in PLATFORMS or (local and local.startswith("upstream/"))):
                target = local.split("/")[-1]
                if target in PLATFORMS and target != platform:
                    reason = "platforms must not import each other"
            if relative != "app" and top in {"app", "server", "httpapi"} and relative not in {"server", "httpapi"}:
                reason = "lower modules must not import application or HTTP assembly"
            if top == "store" and owner != "store":
                reason = "production code must not depend on the deprecated Store facade"
            if owner == "store" and local and top != "storage":
                reason = "deprecated Store facade must delegate only to storage"
            if owner == "storage" and (dep == "net/http" or dep.startswith("net/http/")):
                reason = "storage must not own HTTP transport"
            if reason:
                errors.append(f"{name} -> {dep}: {reason}")
    # A legacy helper can hide a forbidden dependency from a direct-only rule.
    # Follow internal edges from each neutral owner; adapters/Store/Registry are
    # intentionally outside these roots and retain their documented bridges.
    graph = {p["ImportPath"]: p.get("Imports", []) for p in packages}
    for root in sorted(graph):
        if not neutral(root.removeprefix(PREFIX)):
            continue
        queue = deque([(root, [root])])
        seen = {root}
        while queue:
            name, path = queue.popleft()
            for dep in graph.get(name, []):
                if dep in seen:
                    continue
                seen.add(dep)
                local = dep.removeprefix(PREFIX) if dep.startswith(PREFIX) else None
                top = local.split("/", 1)[0] if local else None
                forbidden = top in {"server", "httpapi", "app", "store", "storage", *PLATFORMS} or (local and local.startswith("upstream/"))
                forbidden = forbidden or dep == "net/http" or dep.startswith("net/http/")
                forbidden = forbidden or (top == "gateway" and root.removeprefix(PREFIX).split("/", 1)[0] != "gateway")
                if forbidden and len(path) > 1:
                    errors.append(f"{' -> '.join([*path, dep])}: neutral business module has an indirect transport/storage/platform dependency")
                elif local and not forbidden:
                    queue.append((dep, [*path, dep]))
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
    print("Direct and transitive business import boundaries passed (legacy bridges remain documented).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
