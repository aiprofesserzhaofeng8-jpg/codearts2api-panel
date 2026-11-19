# CodeArts and Trae Chat tool calls, 2026-11-19

The local gateway completed a two-turn Chat tool roundtrip for each exact
source below: the model requested a tool, then returned the marker supplied
in its result. The acceptance runner made 25 HTTP requests (one catalog read
and two Chat requests per source). Before the catalog correction, the isolated
gateway returned HTTP 400 with reason `model_capability_unavailable` for every
single-source unified name when the catalog carried no upstream tool flag.

- `codearts/GLM-5.2`
- `codearts/glm-5.3`
- `codearts/glm-5.3-flash`
- `codearts/kimi-k3.1-pro`
- `trae/cn/DeepSeek-V4-Flash`
- `trae/cn/DeepSeek-V4-Pro`
- `trae/cn/glm-5.2`
- `trae/cn/glm-5.3`
- `trae/cn/glm-5.3-flash`
- `trae/cn/mimo-v2.6-pro`
- `trae/cn/minimax-m3.1-flash`
- `trae/cn/step-5-preview`

Only tool calling was verified. These observations do not change image input
or output limits, and do not apply to other variants or regions.
