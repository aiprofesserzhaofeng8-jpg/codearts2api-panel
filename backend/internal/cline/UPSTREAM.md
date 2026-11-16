# Cline source provenance

Repository https://github.com/Wei-Shaw/sub2api
Revision 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
License LGPL-3.0-only (LICENSE and COPYING included).

Adapted cline.go fixed Chat Completions endpoint and three wallet prefixes;
cline_usage.go account envelope, user balance and ClinePass window fields.
Reviewed cline_test.go and ratelimit_cline.go. No SQL, additional scheduler,
wallet cooldown database, account-wide window ban, or balance admission copied.

Current official documentation checked 2026-11-16:
https://docs.cline.bot/api/overview
https://docs.cline.bot/api/authentication
https://docs.cline.bot/api/models

API-key only initially. Account auth-token/OAuth lifecycle is not implemented.
A key may use credits, cline-pass/* and cline-free/* independently. We retain
original model IDs; no automatic paid-wallet fallback or model substitution.
All model requests use /api/v1/chat/completions, including Claude/GPT models.
Public catalog discovery is not authentication: Verify calls users/me only,
without a billable inference; it proves account authentication, not model access.

Balance is exact decimal microcredits divided by 1e6, preserving six decimals,
not upstream's floating-point four-place rounding. 404 plan usage means no
subscription snapshot; other errors fail, never masquerade as zero balance.
Percent and reset values remain unknown when missing. Balance and subscription
windows are separate display-only snapshots.

Unauthenticated observations: /api/v1/models HTTP 200, 453 model rows;
/api/v1/users/me HTTP 401. The public fixture retains only model IDs.
No private API key, real inference or subscription usage has been tested.
Public metadata omits limits/capabilities; do not infer image support from names.

Registered in app/UI with API-key import; Linux/Windows/Docker include notices.
Structured HTTP/SSE quota errors use existing failed-model cooldown only, not
Sub2API wallet-wide cooldown; other models remain eligible for upstream checks.
Plain reasoning maps to reasoning_content; opaque reasoning_details remain in
Chat stream chunks and non-stream aggregation. Chat two-turn tool replay is
covered by isolated HTTP tests. Responses/Messages support an explicit reasoning_details extension, verified
with isolated gateway two-turn replay. This is not native encrypted_content or
signature; clients must preserve unknown fields. Installed Pi Responses
conversion preserves it in an offline test; Messages clients and real upstream
acceptance remain unverified.
