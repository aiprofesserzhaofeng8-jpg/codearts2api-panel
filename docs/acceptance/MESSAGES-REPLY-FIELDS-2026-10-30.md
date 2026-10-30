# Messages 拒答正文与缓存用量修复 · 2026-10-30

已修复并部署。对照版本为 `15cedc0`，修复提交为 `de661cc7f9579de365e6faea7137d42a5b9c4c90`。业务变更仅在 `internal/protocol/anthropic.go` 和 `internal/server/anthropic.go`，集中于 OpenAI → Messages 的响应转换。

运行程序 SHA256 为 `1ccff01a3bd4c0237925bbbbdd38eb8ff2745f9d680d7d3ab018a4620e3da8fd`，PID 3711734，与构建产物一致。部署前在途为 0，已私有备份旧运行程序和状态；普通回退应只恢复程序，保留当前凭据与用量。

## 原因与修复

| 原有问题 | 实施方式 |
| --- | --- |
| 非流式转换 DTO 未读取 `message.refusal`，拒答可能变成空文本 | 增加该字段，将拒答原文作为 Messages 文本块返回；保留已有正文、推理及工具块顺序，字符统计包含拒答 |
| 流式转换同样未读取 `delta.refusal` | 复用正文的文本事件发送方法，立即发送拒答分片；沿用原有结束原因，不新增重试或完整流缓冲 |
| 非流式未读取缓存明细，客户端无法区分普通输入与缓存输入 | 读取 `prompt_tokens_details.cached_tokens`，兼容既有 `prompt_cache_hit_tokens` 别名；按 Messages 语义从输入总量中分离缓存命中，避免重复计算 |
| 0 与缺失用量不能区分，真实 0 被替换成估算 | 使用可区分字段存在性的计数；明确回报 0 时保留 0，只有缺失或 null 才沿用原估算 |

例如上游总输入 12、缓存命中 8、输出 2，客户端得到 `input_tokens=4`、`cache_read_input_tokens=8`、`output_tokens=2`；输入总量仍为 12。网关内部日志、密钥预算和费用仍使用原有计量，不因客户端字段拆分重复扣记。

缓存拆分参考固定 Sub2API 提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2` 的 [chatUsageToAnthropicUsage](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/pkg/apicompat/chatcompletions_anthropic_bridge.go)，并核对 [Anthropic 官方用量定义](https://platform.claude.com/docs/en/build-with-claude/prompt-caching#tracking-cache-performance)。本轮只传递当前适配器已报告的缓存命中，不推算缓存创建量、缓存费用或额外积分消耗。

负缓存值与超过输入总量的缓存值按网关已有计量规则限制到有效区间。没有缓存字段时，不为非流式响应新增一个假定已上报的缓存明细。已完成的大整数工具参数保真继续保留。

## 受控回归

先将回归样本运行在旧转换器，复现拒答丢失、缓存字段遗漏及真实 0 被估算替换；[基线日志](evidence/messages-reply-fields-2026-10-30/baseline-reproduction.log) 中的失败是旧行为复现，不是修复后的回归失败。

33 个针对性叶子用例通过，覆盖：

- 非流式正文、拒答、推理与工具混合响应；工具参数 `9007199254740993` 的原始数字精度。
- 标准缓存字段、别名及优先级、全部命中、明确 0、负数、超过输入总量、缺失与 null 用量。
- 真实双 HTTP 路径：CodeArts 原生非流式、CodeArts SSE 聚合、WorkBuddy 原生 SSE，以及 Trae 的缓存用量，分别经过流式和非流式 Messages。
- 拒答分片先到达真实客户端后，才放行上游结束事件，确认没有等待完整流结束。
- 每请求仅一次上游调用；正文、推理、结束原因、内部总输入/输出/缓存计量、字符统计及租约释放正确。

Trae 的受控原生样本只验证已知的文本与缓存字段，不将它冒充 Trae 原生拒答字段验证。拒答转换由 CodeArts、WorkBuddy 与纯转换测试覆盖。

全量 `go test ./... -race -count=1` 的 13 个有测试包通过；`go vet ./...` 和正式构建通过。

## 真实上游与客户端

部署服务上执行三个上游 × Messages 流式/非流式，共 6 次请求，全部 HTTP 200，预期随机标记、正常结束及用量均正确。逐条按请求 ID 核对：客户端普通输入 + 缓存输入 = 日志总输入，客户端输出与缓存命中也与内部计量完全一致。

实际 WorkBuddy 非流式回报普通输入 254、缓存输入 256，输入合计 510；流式普通输入 252、缓存输入 256，输入合计 508。CodeArts 与 Trae 本批没有缓存命中，不能将 0 命中样本说成验证了它们真实缓存命中场景。

另以本机 Claude Code、正式网关 profile 完成 WorkBuddy 工具闭环：真实读取随机临时文件，最终答案与文件完全一致，3 次请求，5.098 秒。没有测试参数覆盖。

合计 9 次真实请求、9 次上游尝试、9 条日志，全部成功且用量已知。本轮没有专门诱发或捕获真实上游的 `refusal` 字段；这项字段映射由受控原生 HTTP 样本验证，真实请求用于验证正常响应、实际缓存计量及客户端工具未受影响。

## 性能对照

冻结版本和修复版本使用相同基准，`-cpu=1 -benchtime=350ms -count=5`，取中位数。非流式基准包含纯转换及响应 JSON 编码；流式基准处理 100 个分片。均不包含模型或网络时间。

| 路径 | 对照 | 修复后 | 增量 |
| --- | --- | --- | --- |
| 普通非流式文本 | 0.005114 ms | 0.005137 ms | +0.000023 ms，+0.45% |
| 含缓存明细的非流式文本 | 0.005382 ms | 0.005691 ms | +0.000309 ms，+5.74% |
| 非流式工具响应 | 0.006660 ms | 0.006911 ms | +0.000251 ms，+3.77% |
| 100 个流式文本分片 | 0.278247 ms | 0.281698 ms | +0.003451 ms，+1.24% |
| 100 个流式工具分片 | 0.350600 ms | 0.353649 ms | +0.003049 ms，+0.87% |

非流式每请求增加 2～4 次小分配，约 48～112 字节，来自用量字段存在性和缓存字段编码。流式分配次数不变，100 个分片累计增加约 1600 字节。没有新增网络请求、重试、等待、逐片日志或全文解码；未观察到明显延迟退化，但并非零开销。

## 收尾与证据

结束时健康检查 200，12/12 账号 ready，在途为 0；设置与密钥数量保持不变，两轮临时密钥已删除。真实调用产生的用量和日志按正常规则保留。

脱敏证据：[总览](evidence/messages-reply-fields-2026-10-30/summary.json)、[针对性用例](evidence/messages-reply-fields-2026-10-30/controlled-summary.json)、[全量检查](evidence/messages-reply-fields-2026-10-30/checks.json)、[三上游真实接口](evidence/messages-reply-fields-2026-10-30/live-messages.json)、[Claude 工具闭环](evidence/messages-reply-fields-2026-10-30/live-client.json)、[网关日志](evidence/messages-reply-fields-2026-10-30/gateway-logs.json)、[性能对照](evidence/messages-reply-fields-2026-10-30/benchmark-summary.json)、[部署](evidence/messages-reply-fields-2026-10-30/deployment.json)、[结束状态](evidence/messages-reply-fields-2026-10-30/verification.json)。
