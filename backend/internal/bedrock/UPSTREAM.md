# Bedrock wire foundation

Adapted from Wei-Shaw/sub2api at 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484 (LGPL-3.0-only). Original paths and hashes are in upstream.json. LICENSE/COPYING accompany these modified portions. No database, account repository, Gin service, scheduler, beta/model rewrite heuristics or queue were copied.

- signer.go keeps the AWS official SDK v1.41.5 SigV4 wrapper. Account extraction stays for the later adapter. smithy-go v1.24.2 is its transitive dependency; their Apache licenses and notices are included.
- eventstream.go extracts Sub2API's bounded 16 MiB payload / 128 KiB headers CRC decoder. Exception bodies are not copied into errors.
- wire.go adapts model URL escaping, native anthropic_version, and invocation metric fields. It reuses messagewire for Chat to Messages and stream conversion. A lazy reader feeds one frame at a time; no goroutines, full-response buffering, persistence or retry loop.
- Tests use synthetic frames and credentials, not live AWS. They do not establish IAM permissions or model access.

Commercial-region endpoint construction only in this foundation. China/GovCloud partitions, ARN account-region consistency, credential lifecycle, catalog, account import, application registration, retry/error classification, and beta/caching compatibility remain adapter-stage work. The second batch registers the platform with explicit commercial-region credential import; see acceptance record for remaining limitations.

References: https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_InvokeModelWithResponseStream.html and https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages-request-response.html . Update by comparing pinned functions and rerunning tests; preserve source and license notices in distributed source/binaries.

Second batch: client.go implements signed fixed AWS control/runtime HTTP endpoints,
Claude foundation models and bounded system-profile pagination from AWS API docs.
Optional explicit model_ids avoid requiring listing permissions. Verify performs
one short native inference, no retry; credential errors and stream exceptions
are classified without raw messages. Only commercial regions, Claude Messages
models and system profile IDs are supported; ARNs, AssumeRole, host credentials,
automatic STS renewal and Bedrock API bearer keys remain unsupported.
Sources: https://docs.aws.amazon.com/bedrock/latest/APIReference/API_ListFoundationModels.html
and https://docs.aws.amazon.com/bedrock/latest/APIReference/API_ListInferenceProfiles.html .
