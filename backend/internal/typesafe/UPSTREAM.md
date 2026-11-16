# TypeSafe native protocol provenance

Repository https://github.com/Wei-Shaw/sub2api
Revision 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
License LGPL-3.0-only, LICENSE and COPYING included.

systemone.go is adapted from backend/internal/pkg/typesafe/systemone.go.
client.go uses the pinned client's fixed System One path, bounded raw response
and single-attempt transport design. Existing providerhttp supplies safe HTTP.
No moderation service, database, client-side file access or scheduler is copied.

Current official schema reviewed 2026-11-16:
https://api.typesafe.ai/openapi.json
https://docs.typesafe.ai/introduction

Differences from pinned Sub2API:
- Model names/aliases come from authenticated GET /v1/models, not only jev-latest.
- Score criteria permit 1..10 levels per current schema.
- Native responses retain raw bytes; missing/invalid usage remains unknown,
  not rounded/clamped into apparently known zero usage.
- Validate answer presence/type, reject error objects and oversized responses.
- Input is limited to 4 MiB. Native state and question JSON are not recast as
  Chat messages or modified using floating-point decoding.
- First slice supports noul/choice/score only. Assets, Box, multipart and long
  context opt-in are not implemented or silently enabled.

The common provider contract now makes Streaming optional. TypeSafe implements
account operations and native Decision, not Stream; Models returns no chat rows.
Registered with API-key import UI and /v1/systemone, reusing authorization,
atomic admission, credential resolution, leases and ledger with one attempt.
A separate decision catalog UI and full consumer-budget acceptance remain pending.

Official OpenAPI is readable via browser tooling, but local direct HTTPS fetch
failed during TLS handshake. No key or real inference was used. Synthetic tests
verify transport/validation only. Include notices in distribution when exposed.
