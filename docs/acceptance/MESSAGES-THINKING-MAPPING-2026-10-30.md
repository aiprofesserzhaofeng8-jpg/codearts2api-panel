**Messages 关闭思考映射与输入边界 · 2026-10-30**

已补齐 `/v1/messages` 显式 `thinking.type=disabled` 的转换：CodeArts、WorkBuddy 的请求包含同名原生开关，不再在协议 DTO 中被静默丢弃。完整 thinking 支持仍未完成；本次不猜测 Anthropic token 预算对应的上游 effort 档位，不为没有指定思考参数的请求注入任何默认值。

本次验证只使用合成凭据和受控 localhost 上游。它证明字段确实到达出站 HTTP 请求，不能证明各家所有模型都接受或实际执行关闭指令。CodeArts 的既有 DeepSeek 摘要策略已经使用这一原生开关；WorkBuddy 开关来源见下方参考。未修改上游模型默认行为、用户提示词、清洗策略、响应正文或工具参数。

| Messages 输入 | CodeArts / WorkBuddy | Trae |
|---|---|---|
| 省略 thinking，或 thinking=null | 保持原路径，不注入开关或 effort | 保持原路径 |
| thinking={"type":"disabled"} | 原生请求保留该开关，由上游判断模型能力 | 获取账号前 HTTP 400，明确说明当前网关暂不支持此映射 |
| thinking.type=enabled/adaptive | HTTP 400，当前网关尚未实现该映射 | 同左 |
| thinking.budget_tokens | HTTP 400，不静默丢弃预算、不换算为猜测的档位 | 同左 |
| output_config.effort | HTTP 400，当前网关尚未实现映射，包括与 disabled 同时指定的请求 | 同左 |

支持范围刻意限制为关闭思考。Sub2API 的 Anthropic→OpenAI 桥会把关闭偏好转换为 OpenAI 的 reasoning effort；本项目的上游并不是统一的 OpenAI 原生服务，不能直接把该字段名和值套到三个平台。WorkBuddy 的参考实现还显示，显式开启 DeepSeek 需要额外 effort 及多轮推理内容处理，因此本轮没有仅添加一个 enabled 字段就声称开启支持。

**兼容性变化**

以前被忽略的 enabled、adaptive、budget_tokens、output_config.effort 现在会在 Messages 入站返回明确的 `invalid_request_error`，且不会发送上游请求、获取账号租约、重试或惩罚账号。客户端若默认带 `thinking={"type":"adaptive"}, output_config={"effort":"high"}`，或者 `thinking={"type":"enabled","budget_tokens":4096}`，需要省略这些尚未支持的选项；不能将此行为宣称为完整 Claude Code 思考兼容。

这些是相应 API 输入形态的受控 HTTP 回归，不是运行真实 CLI 的验收。Trae 的错误只表达本项目尚未实现控制映射，不断言官方 Trae 本身无法控制思考。

Trae 限制仅加在 Messages HTTP 入口，不修改 Trae 原生适配器。现有 Chat 对未知 thinking 字段的透传、Responses 对其非标准顶层 thinking 字段的既有忽略，以及 Responses 正式 reasoning.effort 转换均保持原状；本轮没有把 Messages 的新输入边界扩散到其他协议。

**验收与性能边界**

- 15 个协议案例覆盖省略/null、disabled、enabled、adaptive、budget、effort、冲突组合及无效类型；省略字段的完整转换结果与原有输出等价。
- 42 个三平台 × 流式/非流式 × 输入偏好场景使用两端真实 localhost HTTP，逐一核对出站开关、不注入默认值、错误状态、上游调用数、账号健康与租约归零。
- 12 个 Trae Chat/Responses 兼容场景覆盖三种 thinking type 与两种流模式，原有行为不变。
- 原冻结版 `bd2196b` 的转换器和 HTTP 入口作 Go overlay 负面对照，43 个子测试失败，复现参数丢失和未支持偏好被记作正常请求。修改后 69 个子测试均在定向 race 测试中通过。
- 新增 `BenchmarkMessagesThinkingConversion`，将请求 JSON 解码与转换一起计时，覆盖短消息/16 KiB 和省略/disabled。它仅调用旧版本已有 API，可原样放入冻结版本对比；此子任务没有运行性能测量，统一性能验收另行执行。

业务改动只在 Messages DTO、独立的小字段转换 helper 和 Messages HTTP 入口；缺失字段时 helper 直接返回。没有增加网络调用、重试、流式缓冲或同步写盘，也不对每个响应分片做任何新处理。

参考：[Sub2API 的思考偏好转换](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/pkg/apicompat/anthropic_to_responses.go)、[WorkBuddy 原生开关及开启条件](https://github.com/linguo2625469/workbuddy2api-panel/blob/73a7560efc7a88ebd9865db4c71ae7f6d85859f8/internal/upstream/thinking.go)。

证据：[负面对照](evidence/messages-thinking-2026-10-30/baseline.jsonl)、[定向竞态验收](evidence/messages-thinking-2026-10-30/fixed-race.jsonl)、[结构化统计](evidence/messages-thinking-2026-10-30/summary.json)。
