# 参照 Sub2API 的网关体系完善

2026-10-26。本项目的定位是单实例码道账号网关，不是 Sub2API 的完整克隆。参考其按上游能力分流、独立压缩路径、有限兜底、完整流式状态机和使用量追踪的设计；具体代码按码道上游条件实现。

## 已实现与边界

| 子系统 | 当前实现 | 与完整 Sub2API 的差异 |
| --- | --- | --- |
| 上游能力 | `/v1/capabilities` 明确声明码道 Chat 上游、Responses/Messages 转换、未知上下文上限和量化信息 | 单一码道上游，不宣称支持其他厂商，也不在普通请求中盲目探测或切换协议 |
| 模型策略 | 管理端可配置每模型上下文、输出、压缩预留、保留历史、触发值和摘要预算 | 默认仅 DeepSeek 有实测保守策略，未知模型不套用其能力 |
| 客户端接入 | Pi、Codex、Claude Code 启动器从 `/v1/client-profile` 获取统一参数 | 客户端需启动器或正确配置，不能由服务端强行修改其本地上下文逻辑 |
| 协议转换 | 原生 Chat、基于固定版 Sub2API 的 Responses 转换、Messages 转换 | 格式转换不产生上游原生托管能力 |
| 文本摘要 | 单独识别、关闭额外思考（按策略）、精简提示、输出预算、完整性验证 | 自动识别仅覆盖已验证的 Pi 系统提示；其他客户端可显式发送摘要用途请求头 |
| 摘要重试 | 截断时同账号最多一次重试，允许管理员显式选择摘要备用模型；先缓冲完整结果再输出 | 默认原模型重试；仅配置后切换，仍非原生 compact |
| 错误处理 | 截断两次、空摘要、缺少结束帧等返回 422，不污染账号健康状态；普通已输出流不重放 | 客户端仍可能自行提交新的请求，服务端不把无限客户端重试当作一个请求 |
| 使用量 | 累计所有已报告的尝试用量、记录最后结束原因及每次尝试明细、按 USD 价格表估算 | 没有上游 usage 的尝试无法推算真实 Token 或真实华为扣款 |
| 观测 | 请求 ID、摘要/普通请求、尝试次数、结束原因、摘要失败/重试指标 | 没有引入 Redis、分布式追踪或多实例统一状态 |
| 账号池 | 并发租约、粘性会话、冷却、熔断、换号和请求级超时 | 只管理码道账号，不覆盖多供应商账号路由 |
| 安全 | 管理与 API key 分离、按需显密、重定向限制、日志脱敏、管理认证失败限流 | 默认强制登录、密码慢哈希、8 小时可撤销会话；上游凭据仍需可逆读取，未引入多用户/2FA |

不支持的 `/v1/responses/compact`、响应存储子路径等现在返回经过鉴权的 **501 unsupported_capability**，不会落到普通页面或伪装成成功。`store=true`、`previous_response_id` 等已有请求校验继续生效。

## 摘要完整性与有限重试

1. 请求必须是已验证的摘要系统提示，或携带 `X-CodeArts-Request-Kind: summary`；带工具定义的请求不匹配。
2. 仅在模型策略的 `summary_enabled=true` 时启用。
3. 使用同一账号租约和同一请求总超时；默认仍用原模型，仅管理员配置摘要备用模型后才在重试时切换。
4. 上游始终流式生成，但摘要的所有分片暂存在内存，最大 8 MiB；不提前发送正文。
5. 只有 `finish_reason=stop`、非空文本、无工具调用时才返回。
6. `length` 且策略允许时最多再生成一次，更强调短摘要；不擅自提高客户端给出的 Token 上限。
7. 再次截断则返回 HTTP 422，客户端收到的是明确失败，不会将部分摘要当作检查点。上游错误不会触发摘要内部换号循环。
8. 缓冲的代价是摘要请求不会即时显示逐字输出，网关前的代理读超时应覆盖请求超时。

**费用说明：**摘要重试会消耗真实上游额度。日志 usage 累加两次已报告用量；最终响应只携带成功那次输出。即使摘要最终失败，已报告的摘要生成用量也按价格表估算 USD 参考费用，不会因 HTTP 422 被当作零消耗。仍不是华为账单。

## 模型策略配置

面板「模型 → 客户端兼容与压缩策略 → 编辑」。保存为 `model_policies`，原子写入状态文件。校验要求：

- 输出小于上下文。
- 预留至少为最大输出 + 4096。
- 压缩触发值不超过上下文减预留。
- 保留近期历史小于压缩触发值。
- 摘要预算不大于输出上限。

修改后重启客户端启动器获取新策略；正在运行的客户端不会自动被服务端改配置。
`/v1/models` 同时附带 `client_profile`，与启动器读取的配置一致。该字段明确表示客户端推荐，不将未知上游最大上下文标为已知。

管理 API：`GET/POST /admin/api/model-policies`。公共能力和客户端配置接口仍需调用 API key。

## 运行记录与持久化

请求日志和独立用量历史新增：

- `request_id`（同时返回 `X-Request-ID`）；
- `request_kind`：inference / summary；
- `finish_reason`：stop / length / tool_calls 等；
- `attempts`、`summary_attempts`；
- `attempt_details`：每次尝试的账号显示名、模型、结束原因和 usage；
- `usage_incomplete`：存在没有返回 usage 的尝试，汇总只计已报告部分，不能当作完整成本。

不记录提示词、生成正文、API key 或上游凭据。多次尝试以整个逻辑请求计一次请求数，Token 则累计所有有 usage 的尝试。缺少 usage 的尝试在明细中保持 known=false。
状态文件每 30 秒原子保存一次，并在正常关闭时保存；这不是逐请求事务数据库，异常断电仍可能损失最后一个保存周期的数据。

Prometheus 新增 `codearts2api_summary_requests_total`、`codearts2api_summary_failures_total`、`codearts2api_summary_retries_total`、`codearts2api_truncated_requests_total`。

## 验证方法

- 三种协议的流式/非流式摘要截断→短摘要成功：不泄出第一份不完整结果，准确累计重试 usage。
- 两次截断：最多两次、返回 422、账号不熔断、费用和尝试明细保留。
- 关闭重试、关闭策略、显式用途、未知模型、普通请求隔离。
- 三种协议的普通流和摘要都要求明确的上游结束标记，提前断流不会返回正常完成。
- Messages 的 length 映射为 max_tokens，输出 usage 使用上游实际值。
- 模型策略持久化与相互约束校验、API key 鉴权、原生不支持能力显式错误。
- 模型策略编辑弹窗、保存失败、取消、移动布局和时间分页回归。
- 实际 Pi 会话副本通过本机网关完成摘要；结果保存在 artifacts，不修改用户原会话。

## 参考源码

本轮设计参考（不是未经区分的全量复制）：

- https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/pkg/openai_compat/upstream_capability.go
- https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/openai_compact_fallback.go
- https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/openai_gateway_responses_chat_fallback.go

已有直接复用的 Responses 转换库仍固定于 `internal/responsescompat/upstream.json` 中的提交和哈希，许可证不变。

## 尚未等同于 Sub2API 的部分

多用户/组织、充值支付、分组定价、多用户 JWT/2FA 管理登录、独立密钥加密、数据库/Redis、多实例、供应商原生 compact、自动全模型能力探测没有在本轮引入。Codex 的工具执行曾被本机 sandbox 限制，不能据此声称所有客户端所有功能已经完整验收。以上需独立实现和测试，不能把这一轮修复称为“完整 Sub2API 平替”。

## 后续管理增强

本轮增加正式管理会话与密码哈希、凭据续期代际检查、账号模型健康隔离、管理审计、摘要备用模型、API Key 请求/Token/USD 预算。详细边界见 [MANAGEMENT.md](MANAGEMENT.md)。不引入分布式锁或支付账本。
