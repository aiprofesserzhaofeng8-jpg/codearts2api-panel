# Gemini 签名工具跨协议补充验收

后续更新：CLI OAuth/原生传输已实现并通过隔离验收，见 [P2b记录](SUB2API-PLATFORM-P2B-2026-11-15.md)。下文保留此前验收时点，Pi签名和真实Google验证仍未完成。

2026-11-15。网关转换修复已实现；本机 Pi 客户端存在元数据丢失，不能宣称端到端签名工具可用。真实 Google 账号、CLI OAuth、生产部署和发布均未执行。

## 本次修复

Google OpenAI 兼容工具调用可能携带 `extra_content.google.thought_signature`。Chat非流式聚合此前已修复；本次继续修复已有 Sub2API 衍生转换器中的类型和投影：

- Responses：工具元数据保留在 function_call 输出项、流式 added/done/completed 项和下一轮 function_call 输入中。
- Messages：工具元数据保留在 tool_use 内容块、流式 content_block_start 和下一轮 tool_use 输入中。
- 不解析签名、不改变工具ID、不混入思考文本、不增加持久化字段、数据库、后台任务或签名缓存。
- Messages流式工具开始后若上游改变元数据，返回错误而不是输出看似成功却无法回放的工具；没有重放请求。同一元数据重复到达可接受。签名在初始工具帧到达的路径已验收。

`extra_content` 是本项目跨协议保留的扩展字段，不是宣称所有 Responses/Messages 客户端都识别它。客户端必须保留并回传该字段才能维持签名连续性。

## 隔离验证

`TestGeminiSignedToolsAcrossProtocols`：Messages/Responses × 流/非流共4组合，真实网关、适配器、共享名称和临时Store，仅上游为httptest。重建客户端历史再请求，逐项断言签名、工具结果、9007199254740993保留，第二轮答复成功，恰好两次上游请求，在途归零。

`TestGeminiSharedChatSignedToolHistory`：Chat签名/图片/工具回归、缓存与推理计量继续通过。

`TestMessagesToolMetadataNeverSilentlyChangesAfterStart`：元数据重复/后改边界，防止静默丢失。

Linux及Windows amd64交叉编译通过（Windows未实机运行）。完整回归与race结果见 `/tmp/codearts-p2-signature-final.log`、`/tmp/codearts-p2-cross-race.log`。新增边界测试首次因测试未设置Flusher失败，修正测试初始化后重新验收；没有改变生产Flush行为。

## Pi 实测（模拟端点，不是Google真实推理）

使用本机已安装 `@earendil-works/pi-coding-agent 1.4.0`，通过临时 `PI_CODING_AGENT_DIR`、虚构API Key及本机HTTP模拟端点；只允许read工具读取临时合成文件，关闭会话/扩展/MCP/skills/上下文文件。未修改用户Pi配置或生产状态。

两轮结果：

```json
{"real_upstream":false,"production_config_changed":false,"pi_exit":0,"completed":true,"requests":[{"request":1,"signature_returned":false,"has_tool_result":false},{"request":2,"signature_returned":false,"has_tool_result":true}]}
```

第二轮有工具结果，但没有回传模拟上游的 `OPAQUE_GEMINI_FIXTURE` 签名。Pi退出码0只证明客户端执行了工具，不能证明真实Gemini会接受缺签名历史。证据 `/tmp/codearts-p2-pi-signature.json`；诊断脚本 `/tmp/codearts-pi-signature-check.py`，临时文件可能被系统清理。

处理边界：继续保持Gemini 3工具能力未确认；后续验证支持保留签名的客户端/原生Gemini客户端通路。用户未要求修改Pi本身，本轮没有编辑全局安装或配置。不得加伪签名、静默换型号或网关签名数据库来冒充兼容。

## P2b 源码调查

额外读取固定 Sub2API `pkg/geminicli/{constants,oauth,token_types,codeassist_types,models,sanitize}.go` 及 Code Assist repository/service，摘要加入现有复用清单。尚未写入可用CLI适配器或开放OAuth按钮。

后续接入应复用已有PKCE会话和ProviderCredentials提交协调；抽取公共OAuth参数与Token/CodeAssist类型，不复制其额外SessionStore清理goroutine或req SDK/仓库层。Code Assist请求使用固定cloudcode端点、`model/project/request`包装，不能将AI Studio API Key端点改成Bearer OAuth就声称支持。项目load/onboard、access token续期和原生请求/响应转换须成套实现后再开放登录入口。
