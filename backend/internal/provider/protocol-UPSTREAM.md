# Protocol routing source

`protocol.go` adapts the pure selection branches of Sub2API
`backend/internal/service/upstream_protocol_routing.go` at
`ba81c7fd758e425d67f856d601147ef0c5263d3f` (Wei-Shaw and contributors,
LGPL-3.0-only; license texts in ../responsescompat/LICENSE and COPYING).

Local differences: account/profile objects are supplied as immutable values;
legacy defaults are explicit; invalid fixed selections fail at normalization;
no probing, cache, account scheduling or network operations are imported.
Endpoint metadata is stored in the existing provider.Model directory.

`protocol_rules.go` adapts `ProtocolRule`, ordered protocol-set matching and
pattern validation from `service/opencode_go.go` at the same revision. Rules
are decoded using typed JSON instead of Sub2API's arbitrary account maps.
Configured but unmatched rules suppress the built-in fallback, preserving the
catalog-first then Chat behavior. Explicit fixed protocol remains authoritative, but a known contradictory model
catalog is rejected locally rather than sent to an incompatible endpoint.
