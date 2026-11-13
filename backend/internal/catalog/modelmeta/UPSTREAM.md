# Sub2API model metadata reuse

Source: https://github.com/Wei-Shaw/sub2api
Revision: 6b4b37db43443b5e88e424fe3c69602cc3810dfb
License: LGPL-3.0, with GPL-3.0 text in COPYING.
Copyright: Wei-Shaw and Sub2API contributors.

`upstream.go` extracts six metadata types and six pure merge/conversion helpers
from `backend/internal/service/upstream_models.go`. Function bodies are copied
verbatim; `upstream.json` records the original file and extraction SHA-256 hashes.
`registry.go` is a local wrapper: bounded JSON parsing, deterministic exact model
lookup, explicit provider selection, and an exported missing-only merge entry.
The source tree and build.sh allow rebuilding with a modified library.

The calling catalog uses only numeric client recommendations. Parsed modality
and reasoning fields do not grant platform capabilities. Public data never adds
gateway models. No Sub2API account types, SQL schema, scheduler, HTTP service,
Codex manifest cache, or protocol conversion was imported into this package.

The optional public registry URL, 8 MiB bound and 6-hour success cache follow
Sub2API. HTTP belongs to `server/model_references.go`; the existing serialized
catalog refresh owns the reference cache, with local 5-second timeout and
30-minute failure backoff. There are no background tasks or persistent files.

Sub2API's generic Codex descriptor uses a 272,000-token fallback. This project
uses a local 32,768-context / 4,096-output compatibility template because its
non-Codex platform channels have different restrictions. Existing measured
recommendations and explicit upstream values take precedence. A compatibility
template is not a measured or guaranteed platform maximum.

When updating, compare the pinned functions and hashes, retain license texts,
and run the modelmeta/catalog/server tests and repository checks. Do not replace
the local authorization or capability rules with native registry declarations.
