# Trae CN / SOLO 原生适配来源

协议参考：

- [connectedGraph/trae2api-web](https://github.com/connectedGraph/trae2api-web)，固定参考提交 `3aabdaa5b02037469a8b019cf1c41d6261a87f49`。主要核对原生端点、请求头、`solo_work_lite` 请求结构、自定义 SSE、授权回调和积分数据。该项目基于 Sliverkiss/traework2api。
- [autumnsentiment/Trae2api-cn](https://github.com/autumnsentiment/Trae2api-cn)，固定参考提交 `bb7b659bdad50ecec9f5eb7686a265d98984b387`。用于交叉核对不同 Trae 通道、令牌字段和本地授权回调约束。

MIT 声明分别保存在本目录的 LICENSE.reference 和 LICENSE.cn-reference。实现按本项目的 provider 接口编写，仅依赖 Go 标准库。

本适配只接入国内 CN/SOLO 的 `/api/agent/v3/llm_utils_chat` 结构化通道，不将 Remote 云端 agent 执行任务当作普通模型 API，也不做模型静默替换、请求内容清洗、TLS 指纹绕过或自动领取权益。

协议参考不是官方 API 稳定性承诺。通过模拟测试不等于真实账号可用：套餐权限、原生通道、client_id 和上游变更均可能影响调用，需要用户自己的有效账号验证。
