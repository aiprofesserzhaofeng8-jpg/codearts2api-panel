# Responses 兼容接口

`POST /v1/responses`，以及 Sub2API 同款简写 `POST /responses`。
Base URL 仍为 `http://127.0.0.1:10087/v1`，使用面板中的调用密钥。

码道上游实际使用 `/api/v2/chat/completions`。此接口在本机转换协议，不会让码道变成 OpenAI 原生 Responses 服务。

## 与 Sub2API 的对齐范围

固定参考版本 `ba81c7fd758e425d67f856d601147ef0c5263d3f`。
直接复用其 Chat fallback 的请求、输出和 SSE 转换代码，源文件 SHA-256 与测试清单保存在
`internal/responsescompat/upstream.json`。来源、LGPL-3.0 许可证和重新构建方法见该目录。

- 字符串 input、消息数组、instructions、developer 角色归一化、图片内容。
- function_call / function_call_output 多轮工具调用及并行工具调用。
- custom/freeform 工具、namespace 工具、由客户端执行的 tool_search、additional_tools。
- reasoning_content / reasoning 与 Responses reasoning summary 的映射。
- JSON 输出格式、max_output_tokens、temperature、top_p、reasoning.effort 等参数转换；具体能力由模型决定。
- 流式 created、output_item、content_part、text、reasoning、工具参数 delta/done 与 completed 事件；序号与零值索引保留。
- usage，包括上游提供时的缓存和推理 token 统计。
- 沿用 Sub2API 当前的长度截断行为：最终事件为 response.completed，但 response.status 为 incomplete，incomplete_details.reason 为 max_output_tokens。
- 成功流末尾同样带 `data: [DONE]`。

接入使用现有鉴权、限流、账号池、凭据续期、会话绑定、请求级超时、重试和日志。
已输出的流遇到错误后发出 response.failed，不重放、不伪造成功。上游无 finish_reason 的断流也视为失败。

码道适配差异：max_output_tokens 转为 max_tokens；始终向上游发流式请求，非流式客户端则等待聚合后的 Responses JSON。
reasoning 项使用按调用密钥隔离的内存缓存（7 天 TTL、最多 2048 项/16 MiB），支持只有 item id 的推理历史回传；重启或淘汰后缓存丢失，客户端应尽量保留完整 summary。

## 使用示例

```bash
export CODEARTS_API_KEY='面板中的调用密钥'
curl http://127.0.0.1:10087/v1/responses \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-v4.1-flash","input":"你好","store":false,"stream":true}'
```

把 stream 改为 false 可得到一次性 JSON；文本在 output 的 message 项 content 中。
使用 OpenAI SDK 时可使用 responses.create，SDK 的 output_text 辅助属性会从 output 提取文本。

多轮：下一次 input 包含先前的用户消息、本次 response.output 及新消息。
工具调用后，把 response.output 中的 reasoning、function_call 一并保留，再追加：

```json
{"type":"function_call_output","call_id":"上次返回的 call_id","output":"工具运行结果"}
```

工具由客户端执行，中转只转换调用格式。
Pi 若使用此接口，provider 的 api 设为 `openai-responses`；原有 `openai-completions` 配置也继续可用。

## 明确不支持的能力

- store=true、previous_response_id、conversation、后台生成和自动上下文管理：返回 400；省略 store 或设为 false，显式携带历史。
- OpenAI 托管工具，例如 web_search、file_search、code_interpreter、image_generation：返回 400；可自行提供客户端 function 工具。
- 原生 Responses WebSocket、响应存储/检索/删除、compact 等子接口。
- 转换层能表达图片、文件等不等于码道模型能接收；图片需选择支持视觉的模型，文件处理不承诺可用。

上游不支持的能力不会因协议转换而自动获得。为避免静默丢弃用户要求，托管工具和有状态请求会比 Sub2API 的宽松转换更严格地拒绝。

## 验证

```bash
go test -race ./...
go vet ./...
python3 scripts/check-sub2api-compat.py
```

最后一项下载上述固定版本的 8 份原始回归测试，在临时 Go 模块中执行（testify 仅用于这项验证，不加入生产依赖）。
`CODEARTS_GO_BIN` 可指定 Go 路径。

真实上游检查（会调用模型并消耗账号额度）：`CODEARTS_LIVE_TEST=1 python3 scripts/responses-smoke.py`。
该脚本验证流式/非流式响应，并在临时 Pi 配置中通过 `openai-responses` 执行只读工具闭环，不修改现有 Pi 配置。
2026-10-25 使用 `deepseek-v4.1-flash` 验证通过，脱敏结果保存至 `artifacts/responses-live-check.json`。

## 2026-10-26 真实验收补充

本次 DeepSeek 上游在指定函数形式的 Chat `tool_choice` 对象下返回 HTTP 400；同一工具定义使用自动选择时，Chat / Responses / Messages 的工具结果往返均通过。不得据此宣称所有强制工具选择参数均已支持。网关不会把强制调用静默改成自动选择。

Pi 按推荐阈值渐进增加的合成会话已完成三次自动压缩；原先一次性批量灌入超过压缩触发点的大段历史会出现摘要预算过小、HTTP 422；启动器新增客户端受控分段恢复后，该复现用例也连续三轮通过。两类场景的原始与修复证据分别保留，超过适配器分段/调用上限时仍须缩减历史。
