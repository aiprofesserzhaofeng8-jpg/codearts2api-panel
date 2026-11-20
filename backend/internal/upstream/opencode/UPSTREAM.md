# OpenCode Zen / Go source provenance

Repository: https://github.com/Wei-Shaw/sub2api
Revision: 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
License: LGPL-3.0-only, LICENSE and COPYING accompany these sources.

Adapted service/opencode_go.go protocol selection and separate Zen/Go mode
semantics; service/opencode_go_usage.go rolling/weekly/monthly field projection.
Reviewed openai_opencode_session.go for the per-conversation header; reuse the existing Request.SessionID without creating new session state.
No upstream SQL/Redis/group snapshots, timer, request-triggered usage job,
account override framework, key grouping cache or distributed lock is copied.

Official documentation checked on 2026-11-16:
- https://opencode.ai/docs/console/models/
- https://opencode.ai/docs/console/go/
- https://opencode.ai/docs/console/api/inference/

Differences from the pinned source: current endpoints use /inference and
/inference/go, with /openai/v1 and /anthropic/v1 native routes. Go MiniMax M3.1
Flash uses Chat Completions, unlike M3 Messages. Zen Qwen3.8 Max uses Chat while
Go uses Messages. Gemini and System One are not implemented in this slice.

Public /inference/v1/models and /inference/go/v1/models were fetched WITHOUT
credentials. They return rich public metadata; they do NOT verify an API key
or subscriber entitlement. New and legacy Go usage paths returned 401 without
credentials, which proves neither payload shape nor a valid account. Tests
retain only consumed fields from the public model snapshots; hashes/counts in
upstream.json identify the observation. No private data or key was fetched.

Credentials have explicit scope zen/go and separate identities. Fixed URLs only;
upstream api.base_url values are never executed. Catalog publishes only entries
whose advertised native protocol matches the implemented routing table. Zen
free entries are excluded per the official outside-client restriction. Known
fields come from the catalog; missing limits/capabilities remain unknown.
Quota preserves missing percent vs 0, unlike upstream's zero-value projection.
Zen balance is unsupported, not fabricated from Go subscription usage.

Generation uses shared stateless Chat/Messages/Responses converters. Verify performs one short authenticated inference after catalog discovery, with no fallback retry; public Models success alone never passes verification. No real account has been tested.
Registered in the application. The import UI separates Zen/Go scopes and discloses the short inference and possible quota consumption.
Linux/Windows packaging and Docker include these licenses/notices. Test from backend: go test -race ./internal/upstream/opencode.

## Protocol routing alignment

`native.go` / `stream.go` use the shared resolver adapted from Sub2API
`upstream_protocol_routing.go` at `ba81c7fd758e425d67f856d601147ef0c5263d3f`.
`protocol_rules` uses the pinned `opencode_go.go` pattern and ordered-set rules.
Catalog protocol metadata stays in the existing model directory. Omitted
api_protocol retains the former model-native route; explicit adaptive enables
inbound preference within supported endpoints. No new cache or endpoint URLs
from catalog data are executed.
