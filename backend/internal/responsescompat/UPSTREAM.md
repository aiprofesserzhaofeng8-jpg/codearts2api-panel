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
