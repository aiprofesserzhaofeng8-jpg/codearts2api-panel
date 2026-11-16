# Shared Google native protocol conversion

LGPL-3.0-only. See accompanying LICENSE and COPYING.

Extracted 2026-11-16 from this repository's Gemini adapter to share content,
image, tool/signature and usage conversion with Antigravity. No credentials,
OAuth, endpoint selection, storage, scheduler or retry ownership lives here.
Request and stream mappings derive from Sub2API revision
530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484, particularly
backend/internal/service/gemini_chat_completions_compat_service.go;
schema.go extracts cleanToolSchema through schemaNumberFloat64 from
backend/internal/service/gemini_messages_compat_service.go.
Repository: https://github.com/Wei-Shaw/sub2api

Original hashes and local paths appear in upstream.json. Gemini's UPSTREAM.md
records the original adaptation. Existing Gemini tests and new Antigravity
adapter/gateway tests exercise this same implementation, including large tool
integers and signed metadata. No model switching or dummy signature is injected.
Build from the source tree with make build. Binary distributions carry these
notices and license texts. This is protocol compatibility, not proof of any
real upstream account entitlement or of a client's signature retention.
