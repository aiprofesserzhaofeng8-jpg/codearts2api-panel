# Grok Build OAuth foundation

Repository: https://github.com/Wei-Shaw/sub2api
Revision: 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
License: LGPL-3.0-only, accompanying LICENSE and COPYING.

Adapted sources:
- pkg/xai/oauth.go: fixed client ID, scopes, authorize/token/callback and Build endpoint.
- repository/grok_oauth_client.go: form-encoded code exchange/refresh without client secret.
- service/grok_oauth_service.go: access-token subject/team metadata and rotation semantics.
- pkg/xai/cli_identity.go and billing.go: fixed CLI request identity/version (1.0.46).
- service/grok_model_catalog.go: observed catalog distinct from configured fallback.

Local changes: use existing providerhttp transport and bounded PKCE flow, not
Sub2API Redis/session cleanup or proxy/unsafe URL settings. A unique nonce is
sent per authorization; ID tokens are neither consumed nor treated as verified
identity. Local JWT decoding extracts metadata only and does not authenticate
users or prove subscription entitlement. Upstream /models must accept the
access token before preview/import verification can succeed. Token redemption
is cached before catalog verification; transient verification failure cannot
redeem the same code twice. Refresh immediately returns rotated credentials;
no additional network check after token rotation. Subject/team cannot change.

Only Build OAuth is implemented here; ordinary xAI API keys, SSO conversion,
password login, CAPTCHA integrations, media generation, fallback models and
cross-client alias replacement are not imported. Catalog preserves actual IDs;
unknown capabilities and limits remain unknown. Registered with the existing application and account UI after isolated
generation/quota/gateway acceptance.
No live xAI account was used. Linux/Windows packaging and Dockerfile carry these component notices/licenses.

upstream.json records upstream SHA-256 and local file digests. Tests run from
backend with go test -race ./internal/upstream/grok. Build the application with make build
once the adapter is complete and registered.

2026-11-16 native generation: stream.go uses the Build /responses endpoint and
CLI headers, following service/openai_gateway_grok.go. Existing ChatGPT Responses
conversion was extracted into sibling responsewire with no credentials, endpoint
or persistence ownership. No cross-model image captioner, model alias rewriting,
SSO/password login, media persistence, extra retry layer or cache is copied.
credit.go follows billing.go weekly/monthly paths and units: weekly percentage
and monthly cents-to-USD only. Missing values remain unknown; endpoint errors
remain errors. Prepaid/on-demand/product breakdowns are not exposed in this slice.
