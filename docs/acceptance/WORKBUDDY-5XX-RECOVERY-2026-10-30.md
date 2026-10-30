# WorkBuddy 临时 5xx 恢复验收 · 2026-10-30

已部署到本机 `codearts2api-panel.service`。本轮修复网关对 WorkBuddy 临时故障的恢复和冷却处理；没有把上游自身偶发 500 的根因标为已消除。

对照版本 `8aa2dd2`；精度修复单独提交为 `e8cec8f`，恢复逻辑为 `bbc7c98`。部署前已将旧运行程序和状态备份到本机私有目录，未提交凭据。部署后 PID 3682397，运行程序与构建产物 SHA256 一致：`e4923df3be2968981f0ae6efa190c7f57198c0f62c54d44c359005ddfed63daa`。

## 修复行为与边界

此前 WorkBuddy 普通 500 与 429 共用模型冷却，默认 60 秒；单账号场景在首次失败后没有可选账号，即使配置了多次尝试，也无法在本次请求内恢复。

| 情况 | 当前行为 |
| --- | --- |
| 首次 WorkBuddy 尝试快速返回 500 / 502 / 503 / 504，未收到分片 | 等待 300 ms，同账号恢复一次。仅在请求开始后 3 秒内能发起恢复时启用，并消耗现有账号尝试次数中的一个名额 |
| 已收到规范化上游分片 | 不触发新增的同账号恢复；文本、工具参数、角色和仅用量帧均算收到分片 |
| 客户端已收到内容 | 沿用原有保护，不换号重放已发送的响应 |
| 持续临时 5xx | 恢复用尽后，沿用现有其他账号选择；对应账号的该模型短冷却，最多 5 秒。新请求继续遵守冷却 |
| 429 / 404 / 凭据故障 / Trae | 保持原有分类和冷却；不套用 WorkBuddy 的新增恢复 |
| 首次失败已经较慢，或网络连接超时 | 不叠加本轮新增恢复；继续由原有超时和账号处理路径处理 |
| 取消或预算不允许继续 | 停止恢复并释放租约，不将取消归为凭据故障 |

新增恢复始终持有原账号租约，不绕过并发限制；等待可被取消。3 秒是**恢复发起窗口**，不替换正常生成请求的超时，不在第 3 秒截断有效输出。成功请求不增加等待、网络调用、正文解码或逐分片日志；每个规范化分片只补一次布尔赋值，用来阻止错误重放。

账号重试层的 `MaxRetries` 保持原有含义，包括首次账号尝试。本轮新增恢复占用其中一个名额；设为 1 时不新增恢复。原有提示词降级和摘要截断重试仍按各自规则工作，不将这些内部机制改造成新的全局重试系统。

每次实际调用分别完成计时和记账。日志新增可选 `attempt_details[].upstream_status`，保留适配器分类后的上游状态，例如上游 500 与公开映射的 502；业务码映射得到的状态也属于适配器状态，不必然等于原始 HTTP 状态。成功和旧日志不出现该字段。仍在整个请求结束时统一写日志。

如果失败尝试没有用量回执，成功恢复后只累计已知用量，并保留 `usage_incomplete`。不能把未知用量当成免费；设置 Token / USD 预算的密钥会在新增调用前重新检查预算，必要时拒绝继续。

## 额外发现的精度缺陷

故障测试发现，原有非流式 OpenAI → Anthropic 工具结果转换将参数解码到 `any`，数字会经过 `float64`。在冻结版本复现：`9007199254740993` 变成 `9007199254740992`；嵌套大整数和高精度小数同样受损。

该问题与 500 恢复无关，已单独提交修复：验证并保留工具参数原始 JSON，省去数字浮点转换。有效数值、空对象和 null 均通过回归；无效 JSON 继续使用空对象兜底。三种协议的恢复测试也检查工具参数中的超大整数不会因恢复改变。

## 验证结果

70 项针对性用例通过，客户端到网关、网关到受控 WorkBuddy 上游均使用真实 HTTP 和原生适配器。覆盖：

- Chat / Responses / Messages × 流式 / 非流式，500 后恢复文本与工具参数；请求体与请求关联标识保持一致。
- 500 / 502 / 503 / 504 持续失败；尝试次数为 1 / 2 / 3；单账号短冷却和多账号切换。
- 已收到文本、角色、用量后的故障；取消、预算拒绝、恢复发起窗口过期；正常生成沿用原截止时间。
- 恢复等待期间的账号并发租约；429、404、501、401 与 Trae 的原有冷却不变；两次尝试分别计时、记录状态和累计用量。

全量 `go test ./... -race -count=1`，13 个有测试的包通过；`go vet ./...` 与正式构建通过。

部署服务的真实验收：

| 项目 | 结果 |
| --- | --- |
| WorkBuddy + Pi 工具闭环 | 通过，实际读取随机临时文件，最终文本完全一致，2 次请求，3.553 秒 |
| WorkBuddy + Claude Code 工具闭环 | 通过，实际执行工具并回填，最终文本完全一致，3 次请求，4.555 秒；包含后台请求 |
| CodeArts 普通请求 | 通过，HTTP 200，返回预期标记 |
| Trae 普通请求 | 通过，HTTP 200，返回预期标记 |

总计 7 次真实请求、7 次上游尝试，全部 200，全部有用量回执。WorkBuddy 5 次请求的网关耗时为 1.398–1.896 秒。真实上游没有返回 5xx，因此本轮真实验收证明正常调用和工具闭环未受影响；恢复能力由受控故障验证，不能据此估算真实故障恢复率或声称上游问题已根除。

## 正常路径性能

同一基准代码在冻结版本和修改版本运行，`-cpu=1 -benchtime=350ms -count=5`，取中位数。WorkBuddy 使用真实原生适配器和受控 HTTP 响应，100 个文本分片；开启通常使用的 3 次账号尝试配置。数值不包含真实模型排队和网络延迟。

| 路径 | 对照 | 修改后 | 时间变化 | 分配次数 |
| --- | --- | --- | --- | --- |
| 通用完整流式路径，CodeArts | 0.3349 ms | 0.3351 ms | +0.07% | 1100 → 1100 |
| WorkBuddy 非流式聚合 | 0.6400 ms | 0.6433 ms | +0.51% | 3110 → 3110 |
| WorkBuddy 流式 | 0.6522 ms | 0.6595 ms | +1.12% | 3068 → 3068 |

分配字节变化约 +0.01%～+0.02%，未观察到明显性能退化。这是有限样本下的对照结果，不保证所有负载下零开销；真实临时失败若触发恢复，另有一次 300 ms 等待和一次上游调用。

## 参考与证据

参考固定 Sub2API 提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2` 的错误分类、有限恢复和输出后的保护：[转发重试](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/gateway_forward.go)、[账号切换](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/handler/failover_loop.go)、[写出响应后的保护](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/handler/gateway_handler.go)、[5xx 与凭据、限流分别处理](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/ratelimit_service.go)。Sub2API 的重试规则依平台和账号类型变化；本项目按 WorkBuddy 的单账号场景保守适配，没有直接照搬其重试次数。

脱敏证据：[总览](evidence/workbuddy-5xx-recovery-2026-10-30/summary.json)、[针对性用例](evidence/workbuddy-5xx-recovery-2026-10-30/controlled-summary.json)、[完整检查](evidence/workbuddy-5xx-recovery-2026-10-30/checks.json)、[真实客户端](evidence/workbuddy-5xx-recovery-2026-10-30/live-clients.json)、[网关请求日志](evidence/workbuddy-5xx-recovery-2026-10-30/gateway-logs.json)、[精度复现](evidence/workbuddy-5xx-recovery-2026-10-30/precision-regression.json)、[性能对照](evidence/workbuddy-5xx-recovery-2026-10-30/benchmark-summary.json)、[部署](evidence/workbuddy-5xx-recovery-2026-10-30/deployment.json)、[结束状态](evidence/workbuddy-5xx-recovery-2026-10-30/verification.json)。

收尾时健康检查 200，12/12 账号 ready，在途为 0；配置与密钥数量不变，两轮临时测试密钥已删除。真实测试的用量与请求日志按正常规则保留。
