---
id: protocol
type: module-design
title: protocol —— Anthropic ↔ OpenAI 纯数据转换
status: draft
parent: architecture
references: [adr-0003]
---

# protocol

## 职责

在两套对外协议之间做**纯数据转换**：

- 请求：Anthropic Messages → OpenAI Chat Completions；
- 响应：OpenAI 响应 / SSE → Anthropic `message` 与事件流（`message_start` /
  `content_block_delta` / `message_stop`）；
- 语义：`stop_reason`、usage、思维链、`tool_use` / `tool_result` 的配对与顺序。

## 边界

- 只依赖标准库及现有的纯转换库 `responsescompat`，不碰网络、存储、日志、`context`。输入字节/结构体，输出字节/结构体。Messages 消息转换复用 Sub2API 的函数；协议层保留输入校验、原生历史和控制参数边界。架构检查仅放行这一条纯转换依赖，继续检查其间接依赖。
- **不做 HTTP 判断。** 鉴权、选模型、错误映射都在 `server`。

Messages 的助手历史中，无签名 `thinking` 映射为 `reasoning_content`，多个块按原顺序连接，不混入可见正文或丢弃仅含思考的助手消息。非空签名和 `redacted_thinking` 必须走原生 Messages 路径并保留原块；普通转换明确报错。签名为空或 null 不表示原生签名。显式 thinking enabled/adaptive、output_config 的原有兼容边界不因此放宽。

这条边界是 `adr-0003` 的结论：转换逻辑原本锁在一个依赖 `net/http`、`store`、`codearts`
的 712 行处理器里，只能靠构造整个 Server 来测，而它当时**完全没有测试**。转换出错不会报错，
只会让客户端拿到语义错误的结果 —— 所以它必须能被独立、密集地单测。

## 已知限制

- `EstimatePromptLen` 只是 `len(body)/4` 的量级估计，用于日志与 usage 兜底，
  **不参与计费或鉴权判断**。
