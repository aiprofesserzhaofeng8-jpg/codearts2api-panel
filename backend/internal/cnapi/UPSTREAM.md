# Official domestic API adapters: Sub2API reuse

Reference: https://github.com/Wei-Shaw/sub2api
Revision: 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484
Copyright: Wei-Shaw and Sub2API contributors.
Copied/derived protocol material is distributed under LGPL-3.0. LICENSE contains
that text; COPYING contains GPL-3.0 (retained from our modelmeta distribution,
not a claimed file from the pinned upstream root).

`upstream.go` copies cnProviderResponseIndicatesInsufficientBalance verbatim
from backend/internal/service/ratelimit_cn_providers.go. Package/import adapted;
source and extraction hashes are in upstream.json. This is intentionally the
pure body classifier, not the upstream scheduling service.

`credit.go` adapts field mappings from cn_provider_balance_service.go:
DeepSeek balance_infos[].currency/total_balance and Kimi data.available_balance.
Local changes: invalid/missing/non-finite amounts fail rather than become zero;
no database/singleflight, no synthesized total, separate currencies, no quota
cooldown based on the display snapshot. These mappings and invalid/zero balance
test vectors retain the upstream LGPL terms. client_test.go tests include adapted
missing/empty/invalid and actual-zero balance cases from
cn_provider_balance_service_test.go. Local HTTP and stream implementation uses
our existing providerhttp transport, not Sub2API's Gin/repository/gateway stack.

Four thin platform constructors use the official payg endpoint rules in
service/domain_constants.go and provider_profile.go; MiniMax deliberately uses
https://api.minimax.cn/v1 per current official documentation, rather than the
older api.minimaxi.com in that revision. Chat Completions only upstream; our
existing Messages/Responses conversion remains local. OAuth and Coding Plans
are not implemented by these adapters.

Catalog fallback is limited to HTTP 404/405 for Zhipu/MiniMax, using a small
explicit official list (2026-11-15):
- https://docs.bigmodel.cn/cn/guide/start/model-overview
- https://platform.minimax.cn/docs/api-reference/text-openai-api
DeepSeek and Kimi require their authenticated model catalogs. Import uses a short
real generation when the catalog endpoint is absent, not static data as proof
of credential validity. Catalog authorization failures never fall back.

No generic alias guessing, reasoning-space injection, reasoning cache, file
storage, retry loop, database or scheduler was copied. Existing reasoning history
is preserved verbatim. No official API credentials were supplied for live testing.

To update, compare pinned files and affected tests; retain modifications and
license texts. Rebuild the full application with modified cnapi/platform sources
using build.sh or make build (frontend dependencies are pinned by lockfile).
Source releases include this module and platform constructors; Linux/Windows
packages and container images carry the license/provenance files. Do not
relicense copied code under the repository's root Apache license.

P2a (2026-11-15): Gemini reuses the local Chat Completions transport against
Google's documented compatibility endpoint. Scope defaults to cn and can be set
to api only by the Gemini constructor, never by credential-provided URLs.
Its native paginated model discovery lives in ../gemini, and its own Verify
overrides the domestic /models behavior. No additional Sub2API code was copied
for this reuse; the existing LGPL-derived classifier/credit distribution remains.

P4e Kimi Coding (2026-11-16): fixed `https://api.kimi.com/coding/v1`
profile, scope `coding-cn`, mode `coding`, alongside unchanged Moonshot `cn/payg`.
The optional fixed profile is selected before validation; credentials cannot set
URLs, contradictory scope/mode is rejected, and failures never switch to payg.
`coding_quota.go` adapts the pinned `cn_provider_quota_service.go` Kimi
limits[].detail and usage field mappings into existing QuotaWindow. Missing
amounts stay unknown rather than upstream's numeric-zero fallback. No new
cache, polling loop, database fields or scheduling layer. Existing packaged
cnapi LICENSE/COPYING cover this addition.
Official endpoint reference: https://www.kimi.com/code/docs/

P4e second batch (2026-11-16): Zhipu personal Coding and MiniMax domestic
subscription reuse the same fixed-profile selector. The pinned quota service's
TOKENS_LIMIT/CREDIT_LIMIT priority, explicit units (3/6), MiniMax general bucket,
remaining percentage and enabled-week flag are adapted to standard JSON.
Missing values remain unknown; unlike upstream's reset-order heuristic, unknown
Zhipu units are displayed without a guessed duration. Invalid numbers and
business-error envelopes fail without exposing upstream bodies.

Official sources checked this batch:
- https://docs.bigmodel.cn/cn/coding-plan/tool/others : Coding Chat endpoint.
- https://platform.minimax.cn/docs/api-reference/text-openai-api : domestic Chat endpoint.
- https://platform.minimax.cn/docs/m-plan/faq : current M Plan quota URL
  https://www.minimax.cn/v1/token_plan/remains replaces pinned legacy remains URL.
- https://platform.minimax.cn/docs/m-plan/intro : M3.1 Flash subscription model.

Current MiniMax response shape has not been authenticated/live-verified. Known
model_remains/general fields are parsed; unrecognized response shapes stay
unknown, with no legacy-host fallback or invented plan balance.

## Protocol routing alignment

`protocol.go` adapts `provider_profile.go`, `upstream_protocol_routing.go` and
`domain_constants.go` at `ba81c7fd758e425d67f856d601147ef0c5263d3f`.
The existing client owns account modes, transport and default origins. Missing
api_protocol retains Chat; explicit adaptive selects the inbound native endpoint.
MiniMax retains this project's existing api.minimax.cn origin. No credentials
base_url overrides or second catalog/refresh loop were added.
