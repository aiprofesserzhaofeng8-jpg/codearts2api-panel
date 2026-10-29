# Trae CN / SOLO 原生适配来源

协议参考：

- [connectedGraph/trae2api-web](https://github.com/connectedGraph/trae2api-web)，固定参考提交 `3aabdaa5b02037469a8b019cf1c41d6261a87f49`。主要核对原生端点、请求头、`solo_work_lite` 请求结构、自定义 SSE、授权回调和积分数据。该项目基于 Sliverkiss/traework2api。
- [autumnsentiment/Trae2api-cn](https://github.com/autumnsentiment/Trae2api-cn)，固定参考提交 `bb7b659bdad50ecec9f5eb7686a265d98984b387`。用于交叉核对不同 Trae 通道、令牌字段和本地授权回调约束。
- [mmqz/cpa-multi-plugins 签到协议记录](https://github.com/mmqz/cpa-multi-plugins/blob/9576e59ab536/plugins/trae/upstream/client.go)，用于核对 `req_source` 与 OAuth 客户端产品来源的关系。仅参考请求契约，不采用其随机更换设备及跨产品探测策略。

MIT 声明分别保存在本目录的 LICENSE.reference 和 LICENSE.cn-reference。实现按本项目的 provider 接口编写，仅依赖 Go 标准库。

本适配接入国内 CN/SOLO 的 `/api/agent/v3/llm_utils_chat` 结构化通道及每日签到状态/领取接口，不将 Remote 云端 agent 执行任务当作普通模型 API，不做模型静默替换或 TLS 指纹处理。网关在独立的 `internal/requestpolicy` 模块提供可选提示词和文本清洗，按 Trae 自己的配置应用。联网搜索使用独立 SearXNG 后端，未声称接入 Trae 原生搜索，见 `docs/PROVIDER-FEATURES.md`。签到只在账号状态允许时提交，并通过复查确认结果。

协议参考不是官方 API 稳定性承诺。通过模拟测试不等于真实账号可用：套餐权限、原生通道、client_id 和上游变更均可能影响调用，需要用户自己的有效账号验证。
