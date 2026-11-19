"""Interactive acceptance check against the local panel; passwords stay in memory."""
import argparse
import getpass
import http.cookiejar
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:10087"
TARGET = "gpt-6.2-astra"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


client = urllib.request.build_opener(
    urllib.request.ProxyHandler({}), NoRedirect(),
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
)


def call(path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data, headers={
        "Origin": BASE, "Content-Type": "application/json",
    })
    with client.open(req, timeout=75) as response:
        return json.load(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--probe-only", action="store_true", help="test the saved ChatGPT target without syncing any account")
    opts = parser.parse_args()
    user = input("Panel admin username: ")
    call("/auth/login", {"username": user, "password": getpass.getpass("Panel password: ")})
    attempted = False
    try:
        for n, a in enumerate(call("/admin/api/state")["accounts"], 1):
            if opts.probe_only and a["provider"] != "chatgpt":
                continue
            label = f"{n}. {a['provider']}"
            if not a["enabled"] or a["paused"]:
                print(label, "skipped (disabled or paused)")
                continue
            path = "/admin/api/accounts/models?" + urllib.parse.urlencode({"id": a["id"]})
            try:
                snapshot = call(path) if opts.probe_only else call(path, {})
                if not snapshot.get("snapshot"):
                    print(label, "no saved catalog")
                    continue
                result = snapshot["snapshot"]["list"]
                saved = opts.probe_only or call(path)["snapshot"]["list"] == result
                ids = [m["id"] for m in result["models"] or []]
                kind = result.get("kind") or "unspecified"
                print(label, f"source={kind} models={len(ids)} persisted={saved} target={TARGET in ids}", flush=True)
                if a["provider"] == "chatgpt":
                    print("ChatGPT catalog:", ", ".join(ids), flush=True)
                if saved and not attempted and a["provider"] == "chatgpt" and kind == "live" and TARGET in ids:
                    attempted = True
                    full = "/".join((a["provider"], result["scope"], TARGET))
                    probe = call("/admin/api/models/probe", {"id": a["id"], "models": [full]})
                    print("Target probe:", "PASS" if probe["available"] == probe["total"] == 1 else "FAIL")
                    for row in probe["results"]:
                        if row.get("ok") is not True:
                            error = str(row.get("error") or ("no matching account or scope" if "ok" not in row else "no error detail"))
                            error = re.sub(r"(?i)bearer\s+[^\s\"']+|[\w.+-]+@[\w.-]+|[A-Za-z0-9_.-]{32,}", "[redacted]", error)
                            print("Probe detail:", " ".join(error.split())[:400])
                    if opts.probe_only:
                        break
            except (OSError, KeyError, ValueError, TypeError) as exc:
                print(label, "ERROR:", str(exc), file=sys.stderr)
        if not attempted:
            print("Target probe: NOT RUN")
    finally:
        call("/auth/logout", {})


if __name__ == "__main__":
    main()
