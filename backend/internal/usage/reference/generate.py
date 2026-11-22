"""Regenerate offline token rates from the pinned LiteLLM registry (MIT)."""
from decimal import Decimal
import json
from pathlib import Path
from urllib.request import urlopen

REVISION = "ac7674625ceed298d6c411cc964f23aded63989d"
URL = f"https://raw.githubusercontent.com/BerriAI/litellm/{REVISION}/model_prices_and_context_window.json"
# Only matching gateway products and billing scopes belong in this table.
SCOPES = {"deepseek": "deepseek/cn", "opencode_go": "opencode/go", "gemini": "gemini/api"}
FIELDS = ("input_cost_per_token", "output_cost_per_token", "cache_read_input_token_cost")

def extract(data):
    prices = {}
    for key, row in data.items():
        vendor, sep, native = key.partition("/")
        if not sep or vendor not in SCOPES or row.get("litellm_provider") != vendor:
            continue
        if row.get("mode") != "chat" or any(row.get(field) is None for field in FIELDS):
            continue
        if any(k.startswith(tuple(f + "_above_" for f in FIELDS)) for k in row):
            continue
        values = [float(row[f] * 1_000_000) for f in FIELDS]
        if all(0 <= v <= 1_000_000 for v in values):
            prices[f"{SCOPES[vendor]}/{native}"] = dict(zip(("input", "output", "cached"), values))
    return prices

if __name__ == "__main__":
    with urlopen(URL, timeout=30) as response:
        data = json.load(response, parse_float=Decimal)
    Path(__file__).with_name("prices.json").write_text(
        json.dumps({"revision": REVISION, "prices": extract(data)}, sort_keys=True, indent=2) + "\n"
    )
