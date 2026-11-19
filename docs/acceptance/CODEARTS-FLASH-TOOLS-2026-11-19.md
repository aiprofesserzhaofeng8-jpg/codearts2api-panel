# CodeArts DeepSeek Flash tool calling

On 2026-11-19 the operator tested the exact source
`codearts/deepseek-v4.1-flash` through the local panel. The model called
`read_marker` with no arguments. After receiving a random marker as the tool
result, it returned the marker in its next reply (`Source tool roundtrip: PASS`).
This verifies tool calling for the unsuffixed source. Image input was not tested.

The v0.4.21 gateway rejected tool requests to the public name
`deepseek-v4.1-flash` when it had only this source in a `qualified_only`
group. The same request produced `model_capability_unavailable` both inside
Docker and on the Linux host. The routing regression test
`TestUnifiedCodeArtsFlashWithTools` exercises that configuration.
