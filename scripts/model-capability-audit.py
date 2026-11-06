#!/usr/bin/env python3
"""Bounded real-model acceptance. Synthetic inputs only; no configuration writes.

The API key is read in memory from CODEARTS_API_KEY or the existing Pi provider.
Reports contain synthetic answers and scrubbed errors, never request credentials.
--live is mandatory because generation consumes upstream quota.
"""
import argparse
import base64
import concurrent.futures
import datetime
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import threading
import time
import urllib.error
import urllib.parse
import urllib.request


FEATURES = (
    "chat_text", "chat_stream", "responses_text", "responses_stream",
    "messages_text", "messages_stream", "chat_tools", "responses_tools",
    "messages_tools", "image", "image_tool_result", "json_schema",
)
LIMIT = 8 * 1024 * 1024
OUTPUT_TOKENS = 512


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def save(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temp = path.with_suffix(path.suffix + ".tmp")
    temp.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    temp.chmod(0o600)
    temp.replace(path)


def scrub(value, key=""):
    text = str(value)
    if key:
        text = text.replace(key, "[REDACTED]")
    return re.sub(r"(?:sk-ca-|github_pat_)[A-Za-z0-9_]+", "[REDACTED]", text)


def events(raw):
    out = []
    for block in re.split(r"\r?\n\r?\n", raw.strip()):
        data = "\n".join(line[5:].lstrip() for line in block.splitlines() if line.startswith("data:"))
        if data and data != "[DONE]":
            out.append(json.loads(data))
    return out


def parsed(protocol, raw, stream=False):
    """Normalize protocol responses, retaining terminal/error/tool evidence."""
    obj = json.loads(raw) if not stream else None
    ev = events(raw) if stream else []
    if obj and obj.get("error"):
        raise ValueError("response error: " + json.dumps(obj["error"], ensure_ascii=False))
    for event in ev:
        if event.get("error") or event.get("type") in ("error", "response.failed"):
            raise ValueError("stream error: " + json.dumps(event, ensure_ascii=False))
    text, calls, usage, finish, terminal = "", [], {}, None, False
    assistant = None
    if protocol == "chat":
        if stream:
            tools = {}
            reasoning = {}
            for event in ev:
                if event.get("usage"):
                    usage = event["usage"]
                for choice in event.get("choices", []):
                    delta = choice.get("delta", {})
                    text += delta.get("content") or ""
                    for field in ("reasoning_content", "reasoning"):
                        if isinstance(delta.get(field), str):
                            reasoning[field] = reasoning.get(field, "") + delta[field]
                    if choice.get("finish_reason"):
                        finish = choice["finish_reason"]
                    for item in delta.get("tool_calls", []):
                        call = tools.setdefault(item.get("index", 0), {"id": "", "type": "function", "function": {"name": "", "arguments": ""}})
                        if item.get("id"):
                            call["id"] = item["id"]
                        for field in ("name", "arguments"):
                            call["function"][field] += item.get("function", {}).get(field) or ""
            calls = list(tools.values())
            terminal = any(line.strip() == "data: [DONE]" for line in raw.splitlines()) and finish is not None
            assistant = {"role": "assistant", "content": text or None, "tool_calls": calls}
            assistant.update(reasoning)
        else:
            choices = obj.get("choices", [])
            if choices:
                assistant = choices[0].get("message", {})
                text = assistant.get("content") or ""
                calls = assistant.get("tool_calls") or []
                finish = choices[0].get("finish_reason")
            usage = obj.get("usage") or {}
            terminal = finish is not None
    elif protocol == "responses":
        if stream:
            completed = [e for e in ev if e.get("type") in ("response.completed", "response.incomplete")]
            obj = completed[-1].get("response", {}) if completed else {}
            terminal = bool(completed)
        else:
            terminal = "status" in obj
        finish = obj.get("status")
        usage = obj.get("usage") or {}
        assistant = obj.get("output", [])
        for item in assistant:
            if item.get("type") == "function_call":
                calls.append(item)
            for part in item.get("content", []):
                if part.get("type") == "output_text":
                    text += part.get("text", "")
    else:
        if stream:
            blocks = {}
            for event in ev:
                kind = event.get("type")
                if kind == "message_start":
                    usage.update(event.get("message", {}).get("usage") or {})
                elif kind == "content_block_start":
                    blocks[event["index"]] = {**event["content_block"]}
                elif kind == "content_block_delta":
                    delta = event.get("delta", {})
                    block = blocks.setdefault(event["index"], {})
                    if delta.get("type") == "text_delta":
                        block["text"] = block.get("text", "") + delta.get("text", "")
                    elif delta.get("type") == "input_json_delta":
                        block["_json"] = block.get("_json", "") + delta.get("partial_json", "")
                    elif delta.get("type") == "thinking_delta":
                        block["thinking"] = block.get("thinking", "") + delta.get("thinking", "")
                    elif delta.get("type") == "signature_delta":
                        block["signature"] = block.get("signature", "") + delta.get("signature", "")
                elif kind == "message_delta":
                    finish = event.get("delta", {}).get("stop_reason")
                    usage.update(event.get("usage") or {})
            assistant = [blocks[k] for k in sorted(blocks)]
            for block in assistant:
                if "_json" in block:
                    block["input"] = json.loads(block.pop("_json"))
            terminal = bool(ev and ev[-1].get("type") == "message_stop" and finish)
        else:
            assistant = obj.get("content", [])
            usage = obj.get("usage") or {}
            finish = obj.get("stop_reason")
            terminal = finish is not None
        text = "".join(x.get("text", "") for x in assistant if x.get("type") == "text")
        calls = [x for x in assistant if x.get("type") == "tool_use"]
    return {"text": text, "calls": calls, "usage": usage, "finish": finish, "terminal": terminal, "assistant": assistant}


def positive_usage(usage):
    return any(isinstance(usage.get(k), (int, float)) and usage[k] > 0 for k in ("prompt_tokens", "completion_tokens", "input_tokens", "output_tokens", "total_tokens"))


class API:
    def __init__(self, base, key, timeout=50, max_requests=1800, min_interval=0.35):
        self.base, self.key, self.timeout = base.rstrip("/"), key, timeout
        self.lock, self.count, self.max_requests = threading.Lock(), 0, max_requests
        self.min_interval, self.next_at = min_interval, 0

    def request(self, path, body=None, method=None, content_type=None, _rate_retries=0):
        with self.lock:
            if self.count >= self.max_requests:
                return {"status": 0, "error": "test request budget exhausted", "seconds": 0}
            self.count += 1
            delay = max(0, self.next_at - time.monotonic())
            if delay:
                time.sleep(delay)
            self.next_at = time.monotonic() + self.min_interval
        start = time.monotonic()
        data = body if isinstance(body, bytes) else None if body is None else json.dumps(body).encode()
        headers = {"Authorization": "Bearer " + self.key, "Content-Type": content_type or "application/json", "anthropic-version": "2023-06-01"}
        req = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        result = {}
        try:
            try:
                resp = urllib.request.urlopen(req, timeout=self.timeout)
            except urllib.error.HTTPError as error:
                resp = error
            with resp:
                result.update(status=resp.code, request_id=resp.headers.get("X-Request-Id"), content_type=resp.headers.get("Content-Type", ""), retry_after=resp.headers.get("Retry-After"))
                chunks, total = [], 0
                while True:
                    chunk = resp.read1(65536)
                    if not chunk:
                        break
                    total += len(chunk)
                    if total > LIMIT or time.monotonic() - start > self.timeout:
                        raise TimeoutError("bounded response read exceeded time or size")
                    chunks.append(chunk)
                result["raw"] = scrub(b"".join(chunks).decode(errors="replace"), self.key)
        except Exception as error:
            result.update(status=result.get("status", 0), error=scrub(type(error).__name__ + ": " + str(error), self.key))
        result["seconds"] = round(time.monotonic() - start, 3)
        # Only this gateway's pre-admission limiter is safe to retry here.
        # Upstream 429 is retained, never hidden by automatic test retries.
        if result.get("status") == 429 and "该密钥已触发网关限流" in result.get("raw", "") and _rate_retries < 2:
            try:
                delay = min(30, max(1, int(result.get("retry_after") or 1)))
            except ValueError:
                delay = 1
            time.sleep(delay + 0.15)
            final = self.request(path, body, method, content_type, _rate_retries + 1)
            final.setdefault("gateway_rate_limit_attempts", []).insert(0, evidence(result))
            return final
        return result


def payload(protocol, model, messages, stream=False):
    # No provider-specific thinking/effort extension in a cross-provider probe.
    # E.g. Kiro explicitly rejects thinking; testing that is a different case.
    body = {"model": model, "stream": stream}
    if protocol == "responses":
        body.update(input=messages, max_output_tokens=OUTPUT_TOKENS, store=False)
        return "/v1/responses", body
    body.update(messages=messages, max_tokens=OUTPUT_TOKENS)
    if protocol == "chat" and stream:
        body["stream_options"] = {"include_usage": True}
    return ("/v1/messages" if protocol == "messages" else "/v1/chat/completions"), body


def evaluated(response, protocol, stream=False):
    if response.get("status") != 200 or response.get("error"):
        return None, response.get("error") or response.get("raw", "")[:1400]
    try:
        return parsed(protocol, response["raw"], stream), None
    except (ValueError, KeyError, TypeError, AttributeError) as error:
        return None, str(error)[:1400]


def evidence(response, result=None):
    out = {k: v for k, v in response.items() if k != "raw"}
    if result is not None:
        out.update(answer=result["text"][:1000], finish=result["finish"], terminal=result["terminal"], usage=result["usage"], tool_calls=result["calls"])
    if response.get("raw") and (response.get("status") != 200 or result is None):
        out["error_body"] = response["raw"][:1800]
    return out


def text_case(api, model, protocol, stream):
    marker = "CONST_" + secrets.token_hex(5)
    # A tiny code-review fixture also verifies explicit prior-turn context.
    messages = [{"role": "user", "content": "Review this Python fixture: EXPECTED = '" + marker + "'. Remember its exact value."}, {"role": "assistant", "content": "I have noted the EXPECTED constant."}, {"role": "user", "content": "Output only the exact value of EXPECTED from the prior code. No explanation."}]
    path, body = payload(protocol, model, messages, stream)
    response = api.request(path, body)
    result, error = evaluated(response, protocol, stream)
    checks = {"answer": bool(result and marker in result["text"]), "terminal": bool(result and result["terminal"] and result["finish"] in ("stop", "end_turn", "completed")), "usage": bool(result and positive_usage(result["usage"]))}
    return {"status": "PASS" if all(checks.values()) else "FAIL", "expected": marker, "checks": checks, "error": error, "requests": [evidence(response, result)]}


def tools(protocol, image=False):
    name = "read_fixture_image" if image else "read_fixture"
    schema = {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"], "additionalProperties": False}
    desc = "Read the synthetic test fixture. Returns an image." if image else "Read the synthetic Python fixture and return the value stored in it."
    if protocol == "chat":
        return name, [{"type": "function", "function": {"name": name, "description": desc, "parameters": schema}}]
    if protocol == "responses":
        return name, [{"type": "function", "name": name, "description": desc, "parameters": schema}]
    return name, [{"name": name, "description": desc, "input_schema": schema}]


def image_fixture():
    from PIL import Image, ImageDraw, ImageFont
    marker = "".join(secrets.choice("ABCDEFGHJKLMNPQRSTUVWXYZ23456789") for _ in range(7))
    picture = Image.new("RGB", (640, 260), "white")
    draw = ImageDraw.Draw(picture)
    draw.rectangle((25, 30, 120, 125), fill="blue")
    font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 52)
    draw.text((150, 60), marker, font=font, fill="black")
    data = io.BytesIO()
    picture.save(data, format="PNG")
    return marker, base64.b64encode(data.getvalue()).decode()


def tool_case(api, model, protocol, image=False):
    name, definitions = tools(protocol, image)
    fixture = "probe.png" if image else "probe.py"
    prompt = "For this coding test call " + name + " with path '" + fixture + "'. "
    prompt += "Then transcribe the exact characters and square color in the returned test image." if image else "After reading it output only the exact CHECK constant value from the tool result. Do not invent a value."
    path, body = payload(protocol, model, [{"role": "user", "content": prompt}], stream=True)
    body["tools"] = definitions
    first = api.request(path, body)
    result, error = evaluated(first, protocol, True)
    records = [evidence(first, result)]
    if not result or not result["calls"]:
        return {"status": "FAIL", "error": error or "no structured tool call", "requests": records}
    if not result["terminal"] or result["finish"] not in ("tool_calls", "tool_use", "completed"):
        return {"status": "FAIL", "error": "incomplete tool-call stream", "requests": records}
    if len(result["calls"]) != 1:
        return {"status": "FAIL", "error": "expected exactly one fixture tool call", "requests": records}
    call = result["calls"][0]
    try:
        actual_name = call["function"]["name"] if protocol == "chat" else call["name"]
        args = json.loads(call["function"]["arguments"] if protocol == "chat" else call["arguments"]) if protocol != "messages" else call["input"]
        call_id = call["call_id"] if protocol == "responses" else call["id"]
        if actual_name != name or args.get("path") != fixture or not call_id:
            raise ValueError("wrong tool name, arguments or missing call ID")
    except (KeyError, TypeError, ValueError) as error:
        return {"status": "FAIL", "error": str(error), "requests": records}
    marker, png = image_fixture() if image else ("CHECK_" + secrets.token_hex(6), None)
    tool_text = "Read image fixture." if image else "CHECK = '" + marker + "'"
    if protocol == "chat":
        body["messages"] += [result["assistant"], {"role": "tool", "tool_call_id": call_id, "content": tool_text}]
        if image:
            # OpenAI tool-result content is text; image lives in a follow-up user block,
            # matching Pi's read-image conversion path, never pretending native files.
            body["messages"].append({"role": "user", "content": [{"type": "text", "text": "Image returned by " + name}, {"type": "image_url", "image_url": {"url": "data:image/png;base64," + png}}]})
    elif protocol == "responses":
        body["input"] += result["assistant"] + [{"type": "function_call_output", "call_id": call_id, "output": tool_text}]
    else:
        body["messages"] += [{"role": "assistant", "content": result["assistant"]}, {"role": "user", "content": [{"type": "tool_result", "tool_use_id": call_id, "content": tool_text}]}]
    second = api.request(path, body)
    final, error = evaluated(second, protocol, True)
    records.append(evidence(second, final))
    good = bool(final and marker in final["text"] and final["terminal"] and final["finish"] in ("stop", "end_turn", "completed"))
    if image:
        good = good and ("blue" in final["text"].lower() or "蓝" in final["text"])
    return {"status": "PASS" if good else "FAIL", "expected": marker, "error": error or (None if good else "tool result not consumed or incomplete generation"), "requests": records}


def image_case(api, model):
    marker, png = image_fixture()
    parts = [{"type": "text", "text": "For a UI screenshot test, transcribe the characters and name the square color in this generated image. Reply briefly. Do not use tools."}, {"type": "image_url", "image_url": {"url": "data:image/png;base64," + png}}]
    path, body = payload("chat", model, [{"role": "user", "content": parts}])
    response = api.request(path, body)
    result, error = evaluated(response, "chat")
    good = bool(result and marker in result["text"] and ("blue" in result["text"].lower() or "蓝" in result["text"]) and result["terminal"])
    return {"status": "PASS" if good else "FAIL", "expected": marker, "error": error or (None if good else "random image content not recognized"), "requests": [evidence(response, result)]}


def schema_case(api, model):
    marker = "JSON_" + secrets.token_hex(5)
    schema = {"type": "object", "properties": {"marker": {"type": "string", "enum": [marker]}, "value": {"type": "integer", "enum": [7]}}, "required": ["marker", "value"], "additionalProperties": False}
    path, body = payload("chat", model, [{"role": "user", "content": "For a JSON parser unit test, return ONLY an object with marker='" + marker + "' and value=7. No markdown."}])
    body["response_format"] = {"type": "json_schema", "json_schema": {"name": "audit_fixture", "strict": True, "schema": schema}}
    response = api.request(path, body)
    result, error = evaluated(response, "chat")
    try:
        good = result and json.loads(result["text"]) == {"marker": marker, "value": 7} and result["terminal"]
    except ValueError:
        good = False
    return {"status": "PASS" if good else "FAIL", "error": error or (None if good else "response failed fixture schema validation"), "limitation": "A matching sample does not prove server-side schema enforcement.", "requests": [evidence(response, result)]}


def gateway_checks(api):
    checks = []
    for path in ("/v1/files", "/v1/embeddings", "/v1/images/generations", "/v1/audio/transcriptions", "/v1/audio/speech", "/v1/messages/count_tokens"):
        r = api.request(path, method="GET")
        checks.append({"case": path, "kind": "endpoint_discovery", **evidence(r), "available": r.get("status") not in (404, 501)})
    boundary = "AuditBoundary" + secrets.token_hex(6)
    upload = ("--" + boundary + '\r\nContent-Disposition: form-data; name="purpose"\r\n\r\nuser_data\r\n--' + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="probe.txt"\r\nContent-Type: text/plain\r\n\r\nSynthetic fixture only.\r\n--' + boundary + "--\r\n").encode()
    r = api.request("/v1/files", upload, content_type="multipart/form-data; boundary=" + boundary)
    checks.append({"case": "file_upload_multipart", **evidence(r), "available": r.get("status") == 200})
    model = "deepseek-v4.1-flash"
    negatives = [
        ("messages_document", "/v1/messages", {"model": model, "max_tokens": 64, "messages": [{"role": "user", "content": [{"type": "document", "source": {"type": "base64", "media_type": "text/plain", "data": base64.b64encode(b"SYNTHETIC_DOCUMENT").decode()}}]}]}),
        ("responses_input_file", "/v1/responses", {"model": model, "input": [{"role": "user", "content": [{"type": "input_file", "filename": "probe.txt", "file_data": "data:text/plain;base64,U1lOVEhFVElDX0RPQ1VNRU5U"}]}], "max_output_tokens": 64, "store": False}),
        ("responses_store", "/v1/responses", {"model": model, "input": "Synthetic fixture", "store": True}),
        ("responses_previous", "/v1/responses", {"model": model, "input": "Synthetic fixture", "previous_response_id": "resp_synthetic_missing"}),
        ("responses_compact", "/v1/responses/compact", {"model": model, "input": []}),
    ]
    for name, path, body in negatives:
        r = api.request(path, body)
        checks.append({"case": name, **evidence(r), "explicit_rejection": r.get("status") in (400, 404, 501)})
    return checks


def model_run(api, row, folder, features=FEATURES):
    model, cases = row["id"], {}
    start = time.monotonic()
    for feature in features:
        # Authentication/availability failure cannot establish absent capabilities.
        first = cases.get("chat_text", {}).get("requests", [{}])[0]
        if feature != "chat_text" and first.get("status") in (0, 401, 402, 403, 429, 502, 503, 504):
            cases[feature] = {"status": "BLOCKED", "error": "initial request failed; see chat_text; capability remains unverified"}
            continue
        try:
            if feature in ("image", "image_tool_result"):
                value = image_case(api, model) if feature == "image" else tool_case(api, model, "chat", image=True)
            elif feature == "json_schema":
                value = schema_case(api, model)
            else:
                protocol, kind = feature.split("_", 1)
                value = tool_case(api, model, protocol) if kind == "tools" else text_case(api, model, protocol, kind == "stream")
            cases[feature] = value
        except Exception as error:
            cases[feature] = {"status": "ERROR", "error": scrub(type(error).__name__ + ": " + str(error), api.key)}
        current = {"model": model, "advertised_images": row.get("supports_images"), "advertised_tools": row.get("supports_tools"), "started_at": now(), "cases": cases}
        save(folder / (hashlib.sha256(model.encode()).hexdigest()[:24] + ".json"), current)
    out = {"model": model, "advertised_images": row.get("supports_images"), "advertised_tools": row.get("supports_tools"), "finished_at": now(), "seconds": round(time.monotonic() - start, 2), "cases": cases}
    save(folder / (hashlib.sha256(model.encode()).hexdigest()[:24] + ".json"), out)
    print(json.dumps({"model": model, "seconds": out["seconds"], "cases": {k: v["status"] for k, v in cases.items()}}, ensure_ascii=False), flush=True)
    return out


def credentials(args):
    key = os.environ.get("CODEARTS_API_KEY")
    if not key:
        provider = json.loads(args.pi_models.read_text())["providers"]["codearts-local"]
        configured = provider["baseUrl"].rstrip("/")
        if configured.removesuffix("/v1") != args.url.rstrip("/").removesuffix("/v1"):
            raise ValueError("Pi provider URL differs; refusing to send its API key")
        key = provider["apiKey"]
        if key.startswith("!") or not key.startswith("sk-"):
            raise ValueError("Use CODEARTS_API_KEY for non-literal Pi credential references")
    return key


def main():
    global OUTPUT_TOKENS
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true", required=True)
    parser.add_argument("--url", default="http://127.0.0.1:10087")
    parser.add_argument("--pi-models", type=Path, default=Path.home() / ".pi/agent/models.json")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--model", action="append", help="Exact optional subset, otherwise all visible public models")
    parser.add_argument("--workers", type=int, default=2, choices=(1, 2))
    parser.add_argument("--timeout", type=int, default=45)
    parser.add_argument("--max-output-tokens", type=int, default=512)
    parser.add_argument("--min-interval", type=float, default=0.35, help="Minimum global interval between HTTP starts, seconds")
    parser.add_argument("--resume", action="store_true")
    parser.add_argument("--features", nargs="+", choices=FEATURES, default=list(FEATURES))
    parser.add_argument("--source-inventory", type=Path, help="Sanitized runtime snapshot with unified_models; test exact configured raw sources")
    args = parser.parse_args()
    if not 16 <= args.max_output_tokens <= 4096:
        parser.error("--max-output-tokens must be 16..4096")
    OUTPUT_TOKENS = args.max_output_tokens
    os.umask(0o077)
    args.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    if not 0.05 <= args.min_interval <= 30:
        parser.error("--min-interval must be 0.05..30 seconds")
    api = API(args.url.rstrip("/").removesuffix("/v1"), credentials(args), args.timeout, min_interval=args.min_interval)
    catalog_response = api.request("/v1/models")
    if catalog_response.get("status") != 200:
        raise SystemExit("Cannot obtain model catalog: " + str(catalog_response.get("status")))
    catalog = json.loads(catalog_response["raw"])
    models = catalog["data"]
    if args.source_inventory:
        definitions = json.loads(args.source_inventory.read_text())["unified_models"]
        ids = {s["id"] for m in definitions if m.get("enabled") for s in m["sources"] if s.get("enabled")}
        models = [{"id": model, "provider": model.split("/")[0]} for model in sorted(ids)]
        catalog = {"data": models, "basis": "configured enabled raw sources; actual presence is tested"}
    if args.model:
        lookup = {x["id"]: x for x in models}
        missing = set(args.model) - lookup.keys()
        if missing:
            raise SystemExit("Unknown model names: " + ", ".join(sorted(missing)))
        models = [lookup[x] for x in args.model]
    catalog_path = args.output / "catalog.json"
    if not catalog_path.exists():
        save(catalog_path, catalog)
    run = {"started_at": now(), "url": api.base, "model_count": len(models), "scope": "configured_raw_sources" if args.source_inventory else "visible_public_models", "features": args.features, "workers": args.workers, "timeout": args.timeout, "max_output_tokens": OUTPUT_TOKENS, "min_interval": args.min_interval, "max_requests": api.max_requests, "no_configuration_writes": True}
    save(args.output / "run.json", run)
    if not (args.output / "gateway.json").exists():
        save(args.output / "gateway.json", gateway_checks(api))
    results, todo = [], []
    for row in models:
        path = args.output / "models" / (hashlib.sha256(row["id"].encode()).hexdigest()[:24] + ".json")
        prior = json.loads(path.read_text()) if args.resume and path.exists() else {}
        if set(prior.get("cases", {})) == set(args.features):
            results.append(prior)
        else:
            todo.append(row)
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        for result in pool.map(lambda row: model_run(api, row, args.output / "models", args.features), todo):
            results.append(result)
            save(args.output / "results.json", {"run": run, "models": results})
    run.update(finished_at=now(), http_requests=api.count)
    save(args.output / "results.json", {"run": run, "models": results})
    summary = {"models": len(results), "http_requests": api.count, "features": {f: {s: sum(r["cases"][f]["status"] == s for r in results) for s in ("PASS", "FAIL", "BLOCKED", "ERROR")} for f in args.features}}
    save(args.output / "summary.json", summary)
    print(json.dumps(summary, ensure_ascii=False), flush=True)
    return 0 if all(r["cases"]["chat_text"]["status"] == "PASS" for r in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
