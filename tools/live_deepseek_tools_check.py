"""Check the deployed panel with the affected group's API key, entered locally."""
import getpass
import json
import secrets
import urllib.error
import urllib.request

from live_account_catalog_check import BASE, NoRedirect

client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
public = "deepseek-v4.1-flash"
source = "codearts/" + public
messages = [{"role": "user", "content": "Call read_marker with no arguments. Then reply with the marker it returns."}]
tools = [{"type": "function", "function": {"name": "read_marker", "parameters": {"type": "object", "properties": {}}}}]


def main():
    key = getpass.getpass("Affected group API key: ").strip()
    if not key:
        raise SystemExit("No key supplied")

    def post(model, body):
        request = urllib.request.Request(BASE + "/v1/chat/completions",
            data=json.dumps(dict(body, model=model)).encode(),
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
        try:
            with client.open(request, timeout=90) as response:
                result = json.load(response)
                print(model, "tools=" + str("tools" in body), response.status, flush=True)
                return result["choices"][0]["message"]
        except urllib.error.HTTPError as exc:
            try:
                error = json.load(exc)["error"]
            except (KeyError, ValueError, TypeError):
                error = {}
            print(model, "tools=" + str("tools" in body), exc.code, str(error.get("reason", ""))[:100],
                  str(error.get("message", "" )).replace(key, "[redacted]")[:180], flush=True)
            if exc.code == 401:
                raise SystemExit("Authentication failed")
            return {}

    control = {"messages": messages, "max_tokens": 16}
    for model in (public, public + "-1017", source):
        post(model, control)
        post(model, dict(control, tools=tools))

    msg = post(source, dict(control, tools=tools, max_tokens=256))
    calls = msg.get("tool_calls") or []
    if len(calls) != 1 or not calls[0].get("id") or calls[0].get("function", {}).get("name") != "read_marker":
        raise SystemExit("Source tool roundtrip: no valid tool call")
    if json.loads(calls[0]["function"]["arguments"]) != {}:
        raise SystemExit("Source tool roundtrip: unexpected arguments")
    marker = secrets.token_hex(6)
    reply = post(source, {"messages": messages + [
        {k:msg[k] for k in ("role", "content", "tool_calls") if k in msg},
        {"role":"tool", "tool_call_id": calls[0]["id"], "content": marker}],
        "tools": tools, "max_tokens": 256})
    if marker not in (reply.get("content") or ""):
        raise SystemExit("Source tool roundtrip: marker not returned")
    print("Source tool roundtrip: PASS")


if __name__ == "__main__":
    main()
