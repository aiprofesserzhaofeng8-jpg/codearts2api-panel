# Command Code provenance

Adapted from https://github.com/Wei-Shaw/sub2api at
530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484 (LGPL-3.0-only):
- backend/internal/service/command_code.go: three native protocol choices, prefixed model IDs.
- backend/internal/service/command_code_usage.go: alpha/whoami organization handling,
  billing credits and five-hour/weekly windows in USD, epoch-millisecond reset times.
- Matching upstream tests reviewed for payload shapes and organization query rules.

No database, scheduler, cooldown, additional cache, auto top-up, or admission
limit is copied. Quota snapshots are display-only. Purchased credits can bypass
subscription windows upstream; never treat a full window as a gateway ban.
Monthly totals use subscriptions period dates plus usage/summary totalMonthlyCredits and returned remaining credit. Unlike the pinned fallback, totalCost is not assumed to mean monthly-plan consumption. Missing values remain unknown; non-auth period failures retain available balances with a partial-query warning.

Official documentation checked 2026-11-16:
https://commandcode.ai/docs/provider
https://github.com/CommandCodeAI/pi-commandcode-provider
Current docs exclude Go-plan external API access. We do not spoof CLI headers.
Paid Provider API and eligible subscription keys use the same global endpoint.
Changing the local billing label does not create a second identity for one key.

Fixed /provider/v1 routes; vendor-qualified names are preserved verbatim in JSON,
never interpolated into request URLs. Catalog supported_endpoints must match the
reviewed route. System One decision models are not chat models and are omitted.
Read context_length and optional max_output_tokens/modalities as declared; do not
copy the Pi plugin's speculative vision/reasoning or 32K output defaults.
Public catalog success is not authentication: Verify requires /alpha/whoami
and one short inference. The import UI must disclose possible quota usage.
Stateless chatwire/messagewire/responsewire provide native conversion.

Local unauthenticated GET /provider/v1/models returned HTTP 403. No successful
live catalog snapshot or credential validation was obtained. Fixtures are
synthetic examples derived from documented fields, not recorded live success.
Registered in app and API-key import UI. LICENSE, COPYING and this manifest are included in Linux/Windows/Docker packaging. Real-account acceptance remains pending.
