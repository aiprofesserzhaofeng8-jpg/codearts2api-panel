# Gemini / Code Assist provenance

Repository: https://github.com/Wei-Shaw/sub2api
Revision: 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
Copyright: Wei-Shaw and Sub2API contributors.
Copied/derived files retain LGPL-3.0 (LICENSE); GPL-3.0 is supplied in COPYING.
The root Apache license does not replace the licenses on these files.

Direct copies, package changed from geminicli to gemini and provenance header added:
- pkg/geminicli/constants.go -> oauth_constants.go (OAuth endpoints/scopes only; client credentials are deployment environment settings)
- pkg/geminicli/codeassist_types.go -> codeassist_types.go
- pkg/geminicli/token_types.go -> token_types.go
- pkg/geminicli/models.go -> cli_models.go (reference catalog, not live entitlements)

Extracted unchanged pure helpers from service/gemini_messages_compat_service.go
into schema.go: cleanToolSchema through schemaNumberFloat64. Dependencies are
standard library only. Tool schema constraints unsupported upstream follow that
pinned converter; numeric function arguments are separately kept as RawMessage.

Local adapters in oauth.go/native.go adapt the flow/endpoint, project and usage
mappings from gemini_oauth_service.go, repository/geminicli_codeassist_client.go,
and gemini_chat_completions_compat_service.go/gemini_messages_compat_service.go.
Local differences: existing bounded PKCE/account coordinators, no Gin/repository/
req SDK, no second scheduler/store/cache; one onboard write followed by bounded
operation reads; never enumerate/choose unrelated cloud projects; default offered
tier only; identity/project verified; rotated tokens returned immediately to CAS
owner; native stream finish required, errors never success; no dummy signature.

The API-key client uses the official OpenAI compatibility endpoint and native
metadata as documented in P2a. CLI calls v1internal with model/project/request,
not the AI Studio API endpoint. Unknown credit and capabilities are not invented.
No live Google account validation has occurred. The local Pi signature limitation
remains; copying native transport does not fix clients that drop tool metadata.

upstream.json records original files and copied local hashes. Rebuild with
build.sh/make build using these sources; source releases include them, and
Linux/Windows/container distributions carry these notices and license texts.

Official protocol cross-check: google-gemini/gemini-cli revision
4ed06ed0ddc523a6d356a4ad35ddfea331041888, packages/core/src/code_assist/server.ts
and types.ts (Apache-2.0). Reference only: GET /v1internal/{operationName},
metadata ideType=GEMINI_CLI (replaces the pinned Sub2API ANTIGRAVITY hint), and
POST retrieveUserQuota with project/userAgent and bucket fields. credit.go is
a local mapping using existing QuotaWindow/QuotaNumber, not a copied SDK.
Hashes are in google_reference. Empty buckets stay unknown, zero remains zero,
percentages do not fabricate a dollar balance or a total quota.

2026-11-16: native request/response and schema conversion moved to sibling
googlewire for Gemini CLI and Antigravity reuse; no endpoint or auth shared.
Relative paths in upstream.json reflect the move. Both component notices ship.

OAuth deployment change: client IDs and secrets are no longer bundled. Read the respective GEMINI_CLI_OAUTH_CLIENT_ID/SECRET or ANTIGRAVITY_OAUTH_CLIENT_ID/SECRET environment settings at client construction. No default fallback. Upstream hashes remain provenance, not local-byte hashes. Tests use synthetic client configuration.
