# Antigravity source provenance — P3a foundation

Source: https://github.com/Wei-Shaw/sub2api
Revision: 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
License: LGPL-3.0-only; LICENSE and COPYING accompany these sources.

- catalog_types.go: copied ModelQuotaInfo through FetchAvailableModelsResponse
  from backend/internal/pkg/antigravity/client.go without changing type bodies.
- client.go: adapted OAuth constants, scopes, callback, User-Agent and endpoints
  from pkg/antigravity/oauth.go and client.go. The installed-application OAuth
  client constant is upstream public code, not a user's account credential.
- oauth.go: adapted token/user/project flow with the project's existing PKCE
  helper and the previous Gemini adapter's bounded transport patterns.
- catalog.go: local projection into provider.Model and accounts.CreditInfo.

Changes: no upstream URL availability cache, fallback loop, repository, SDK or
scheduler. Catalog and future generation use the same fixed daily endpoint;
no account-supplied URL. No raw token/error response logging. PKCE caches code
redemption before downstream validation. Refresh returns rotated credentials
immediately for the existing account coordinator to commit. Missing project during explicit browser login now follows the upstream
default-tier onboarding protocol; import/refresh never enroll. No generated
project ID or guessed subscription tier. Unknown quota remains unknown, not zero.

2026-11-16: native.go adds the Sub2API request envelope (agent request type,
userAgent, session and request IDs, required toolConfig). Content and SSE use
sibling googlewire, retaining signatures and integers. Numeric reasoning
budgets preserve explicit output ceilings. No implicit model switch, identity
rewriting or dummy signature. Registered with existing app/account coordinators;
source and Linux/Windows/container distribution notices are included. Real
Google acceptance is still pending. Run make check / go test from backend.

upstream.json records fetched original files, exact copied fragment and local
adapted source hashes. No model identity prompt rewriting, fallback to another
model or dummy thought signatures have been migrated.

2026-11-16 onboarding: onboard.go adapts resolveDefaultTierID from
service/antigravity_oauth_service.go and Client.OnboardUser from
pkg/antigravity/client.go. Only an offered isDefault tier is selected; at most
five calls spaced two seconds, and only explicit done=false continues. Unlike
the upstream multi-endpoint retry, HTTP/network/invalid responses stop at once.
Cancellation stops polling; terminal project must match an existing identity.
No runtime cache, background job, persistent state or refresh path added.

OAuth deployment change: client IDs and secrets are no longer bundled. Read the respective GEMINI_CLI_OAUTH_CLIENT_ID/SECRET or ANTIGRAVITY_OAUTH_CLIENT_ID/SECRET environment settings at client construction. No default fallback. Upstream hashes remain provenance, not local-byte hashes. Tests use synthetic client configuration.
