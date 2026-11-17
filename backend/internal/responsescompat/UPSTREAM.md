# Sub2API Responses compatibility

Source: https://github.com/Wei-Shaw/sub2api
Revision: ba81c7fd758e425d67f856d601147ef0c5263d3f

The Go files in this directory are derived from backend/internal/pkg/apicompat,
licensed under LGPL-3.0 (LICENSE), incorporating GPL-3.0 (COPYING).
Original authors: Wei-Shaw and Sub2API contributors.

Selected conversion files are copied verbatim; transport, account management and
validation live in internal/server. Source and build.sh allow rebuilding with
a modified compatibility library. See helpers.go for extracted shared helpers.

## Direct Messages message conversion (2026-11-07)

The request message functions in `anthropic_chat_messages.go` are extracted from
Sub2API revision `6b4b37db43443b5e88e424fe3c69602cc3810dfb`:

- `backend/internal/pkg/apicompat/chatcompletions_anthropic_bridge.go`:
  `anthropicMsgToChatMessages`, `anthropicUserToChatMessages`,
  `anthropicAssistantToChatMessages`, `anthropicThinkingToReasoningContent`.
- `backend/internal/pkg/apicompat/anthropic_to_responses.go`:
  `anthropicImageToDataURI`, `convertToolResultOutput`,
  `extractAnthropicTextFromBlocks`.

`messages-upstream.json` records the original source hashes and extraction list.
The original functions use the existing compatibility types; a narrow exported
entry point makes them available to `protocol`. The source is adapted, not an
unmodified copy. Local changes retain URL images (`AnthropicImageSource.URL`),
label tool-result media with the originating call ID, preserve empty tool-result
text, and join tool-result text with the pre-existing single-newline convention.

`protocol` validates content first, rejects unsupported attachments, restores
ordinary reasoning and opaque native history, retains assistant images/text
boundaries and exact tool-result text, and keeps parallel tool results adjacent.
It retains its own top-level control and system-prompt contracts. The extracted
bridge does not own routing, account selection, retries or any transport.

The rest of this package retains the base revision above. Response/SSE conversion
and gateway service code from this newer revision were not imported. Keep the
local regression tests when updating these functions, and review both source
manifests rather than treating the whole package as a single upstream snapshot.

## Gemini tool metadata preservation (2026-11-15)

Local amendments to `types.go`, `chatcompletions_responses_bridge.go`, `responses_stream_event_wire.go`, and
`anthropic_chat_messages.go` preserve optional opaque `extra_content` on tool
calls, Responses output items and Messages tool_use blocks. Stream final items
and replayed input retain the same bytes. This extends the existing copied
converters; no extra upstream files or dependencies were copied. Original
source hashes remain unchanged in the manifests; they describe upstream, not
these amended local files. LGPL terms continue to apply.

This is a gateway extension for clients that retain unknown tool metadata, not
a claim that all official SDKs preserve it. A local Pi fixture showed that its
OpenAI-compatible client drops it. No signature cache, fake signature or model
switching was added to hide that client limitation.

## Opaque reasoning extension (2026-11-16)

Local amendments retain raw `reasoning_details` arrays on Chat messages/deltas
and Responses reasoning output items, including streamed terminal items and
replayed assistant/tool history. They are an explicit compatibility extension,
not native OpenAI encrypted_content. User-turn boundaries clear inherited
opaque data. No server cache, fake signature, decryption or model switch is used.
Original source-file hashes above remain upstream hashes, not modified-local hashes.
Clients that discard unknown item fields cannot use this extension reliably.

## Adapted-source verification (2026-11-16)

`upstream.json.source_files` still records the original upstream hashes.
`adapted_source_files` records the reviewed local hashes and reasons for the
three modified base files. `scripts/check-sub2api-compat.py` checks local bytes
against the appropriate hash, independently fetches/verifies pinned upstream
originals and tests, then runs upstream and local regression tests together.
Downloaded tests use an upstream_ prefix, so local tests are not overwritten.
Unrecorded drift still fails; do not rewrite provenance hashes to bless edits.

`make check` runs `--local-only` without network access. CI and Release run the
full pinned-source and regression check before artifacts can be published.

## Native protocol routing alignment

The existing converters and local fixes remain in use for cross-protocol calls.
Same-protocol Messages/Responses now bypass them. Native transport is adapted
to this project's providerhttp SSE parser, error types, prompt-policy hooks and
usage ledger instead of importing Sub2API's Gin handlers. See
../provider/protocol-UPSTREAM.md for selection/rule extraction and the protocol
alignment plan for the preserved stateful API restrictions.
