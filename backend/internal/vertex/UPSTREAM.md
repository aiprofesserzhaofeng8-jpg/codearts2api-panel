# Vertex AI service-account foundation

Adapted from Wei-Shaw/sub2api at 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484, backend/internal/service/vertex_service_account.go (LGPL-3.0-only).

Reused protocol decisions: fixed Google token endpoint, cloud-platform scope, RS256 assertion with issuer/audience/one-hour lifetime, JWT bearer form exchange, project/location paths and global host handling. Source hash is in upstream.json; LICENSE/COPYING accompany this adaptation.

Local changes: stdlib RSA/x509 signing instead of the upstream JWT library; bounded/redacted token responses with explicit TTL; existing ProviderCredentials lock/CAS persistence instead of Redis/distributed-lock token caching. No ambient credentials, custom token endpoint, retry, API Key/Express mode or Claude Vertex support. The second batch registers the platform and adds JSON import, native Gemini streaming via existing googlewire, and an explicitly configured model_ids catalogue. Import exchanges a fresh token then verifies one short inference before saving. No discovery/permission guarantees are inferred from configured model names.

Official references:
- https://developers.google.com/identity/protocols/oauth2/service-account
- https://cloud.google.com/vertex-ai/generative-ai/docs/model-reference/inference

Tests use generated fixture RSA keys and fake HTTP responses, not real cloud accounts. Keep notices with source/binary distributions; compare the pinned functions before updating.
