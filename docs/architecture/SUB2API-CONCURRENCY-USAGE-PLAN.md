# 对齐 sub2api：预算并发与流式用量修复计划

2026-11-21 保留预算并发分离和部分用量结算；按用户要求撤回第三项“断开后的有界读取”，恢复客户端取消向上游传播。回归入口见文末。

## 目标与既定约束

配置 Token 或费用预算后，实际并发仍由用户、套餐和账号的显式并发配置决定。
原生 Messages 已收到的输入用量必须保留；客户端断开后取消上游，只按已经收到的用量结算。

沿用 [上游失败不冻结用户预算](RATE-LIMIT-SETTLEMENT-PLAN.md) 的已部署规则：缺少用量或历史费用只作信息记录，绝不恢复 Incomplete/Unpriced/ReviewRequired 拦截。实际余额、预算上限、授权、账号冷却和日志持久化故障各自按现有规则处理。保留真实账本，不通过重置额度修复问题。

## 参考版本与证据

最初计划参考 sub2api 提交 `328db26f450edbdf9f8d4344a3ea903ac6396842`。其中断开后读取相关实现仅为历史参考，本项目已撤回该项：

- [Chat Completions 准入](https://github.com/Wei-Shaw/sub2api/blob/328db26f450edbdf9f8d4344a3ea903ac6396842/backend/internal/handler/openai_chat_completions.go)：先按 subject.Concurrency 获取槽位，再检查计费资格；预算不会强制单并发。
- [Anthropic 用量提取](https://github.com/Wei-Shaw/sub2api/blob/328db26f450edbdf9f8d4344a3ea903ac6396842/backend/internal/service/gateway_anthropic_passthrough.go)：message_start 即更新输入和缓存明细，message_delta 合并后续用量。
- [原生 Anthropic 流处理](https://github.com/Wei-Shaw/sub2api/blob/328db26f450edbdf9f8d4344a3ea903ac6396842/backend/internal/service/openai_gateway_messages_anthropic_native.go)：错误携带已收集用量；客户端写失败后继续读取。
- [上游收尾上下文](https://github.com/Wei-Shaw/sub2api/blob/328db26f450edbdf9f8d4344a3ea903ac6396842/backend/internal/pkg/httputil/upstream_drain.go)：保留原客户端取消信号，断开后启动总收尾期限，默认 180 秒；收到新数据不延长期限。
- [Anthropic 读取泵](https://github.com/Wei-Shaw/sub2api/blob/328db26f450edbdf9f8d4344a3ea903ac6396842/backend/internal/service/openai_gateway_anthropic_native_pump.go)：独立读取、超时退出并关闭响应体，避免永久占用连接。

参考行为并复用合适实现；如直接移植代码，保留原许可、版权和来源说明，遵循仓库现有第三方代码约定。

## 已复现的问题

1. 用户并发设为 5、费用预算为 10 美元，第一把密钥有一个在途请求，第二把密钥收到 402“该用户正在结算请求”。第一条结束后恢复。密钥层也存在同类隐式串行判断。
2. 原生 Messages 收到 input_tokens=100、cache_read_input_tokens=20 后中断，当前请求记录仍是 Known=false、InputTokens=0。
3. 客户端取消后 SSE 立即退出，正文后的终结用量事件未被消费。

隔离复现记录：`/tmp/codearts-audit-usage/results.txt`。实施时转成正式回归测试，临时路径不作为长期验收依赖。

## 一、并发与预算独立

### 修改范围

- 删除 storage/budget.go 中密钥 active>0 且设置计量预算时的拒绝分支。
- 删除 `checkUserReservation` 中因 Token 或费用预算且已有在途请求而拒绝准入的分支。`storage/consumer.go` 的订阅准入也调用此函数，密钥、共享用户和订阅三条路径分别验证。
- 保留 budgetActive 对并发槽位和请求次数预留的作用、幂等释放、用户/套餐有效并发、账号最大在途数。
- 保留 consumerReserved 对钱包的原子预留；每条请求使用独立 RequestID，同一请求重试更新同一份预留，不能重复扣占。
- 保留请求次数预算“已用+在途”校验。并发槽位不足使用现有并发错误（429），不可伪装成余额不足。
- 所有真实 Token/费用仍累计到密钥、共享用户和套餐相应账本；下一次准入检查累计数。

### 预算含义

Token/费用上限沿用软上限：已知累计数达到上限后拒绝新请求，已准入请求允许完成并结算。放开并发后，多条在途请求可能一起超过上限；不再宣称“最多超出一条请求”。钱包仍执行现有估算预留，但估算不是精确的最终费用。

本批不另建 Token 估算预留系统，不增默认并发上限，不以预算配置暗中覆盖并发配置。修改现有说明与测试中“计量预算强制单请求”的旧口径。

## 二、部分用量立即保留

### 修改范围

- nativeUsage 收到 message_start 后立即更新请求级已知输入和缓存数值，后续 message_delta 按累计快照更新，不能累加同一快照。
- 用量值和完整性分开表达：部分已知输入可记录、可按已知部分计费；缺失输出保持缺失语义，不能把它标成上游明确报告的零。
- 优先复用现有 TokenUsage、UsageIncomplete 和 RequestCost.Partial 表达已知数值与完整性。实施时先验证这些字段能否在尝试汇总和账本重放中保留部分回执；只有明确无法表达时，才增加最小的向后兼容字段，并记录具体缺口。不得把旧 Known 的含义静默改掉。
- 当前 `server/usage.go` 的 `addUsage` 会跳过 `Known=false` 的尝试，`requestUsageIncomplete` 又仅检查每次尝试的 `Known`。仅在首包填入数字会在汇总时丢数，仅将 `Known` 改为 true 又会把缺失输出当成完整回执。用“一次部分回执、下一次完整回执”的记录验证汇总和重放，已知 Token 各计一次，请求仍标为部分用量。
- 沿 nativeUsage → 请求用量 → 尝试汇总 → 费用结算检查部分值的传递，只修改会丢失或误解部分用量的调用方。captureUsage 和报表仅在受影响时调整，不预设全链路重构。
- Anthropic 总输入按现有契约合并普通输入、缓存读取、缓存创建；缓存读取不重复计费。保留现有平台归一化。
- 完整终结回执覆盖部分快照；每次尝试独立保存，重试汇总各次用量，最终账本只结算一次。
- 流出错时也保留已知输入、输出、缓存与推理明细。未知部分保留信息标记，不冻结账号或用户。

## 三、客户端断开后有界收集回执（已撤回）

2026-11-21 按用户要求撤回此项。客户端断开后恢复原有取消传播，立即取消上游请求，不再设置 180 秒收尾窗口。删除 readStream 的 WithoutCancel、收尾计时器及专用关闭上下文；摘要和流式聚合恢复使用原请求上下文。

已收到的完整或部分用量仍按第一、二项入账；断开之后才返回的用量不再等待，可能缺少最终消费数据，但不会因此冻结用户或恢复隐式单并发。请求原有总超时继续保留。

回归覆盖适配器准备期间取消、HTTP 首包前及输出后断开、流式及缓冲入口、租约释放与下一请求恢复。OfficeAce 补取凭据期间取消的隔离复现必须不再发起推理。

## 实施顺序与验收

| 批次 | 改动与测试 | 完成标准 |
| --- | --- | --- |
| 1 | 预算并发分离 | 账号可用容量、钱包可预留余额和请求次数额度均充足，用户/套餐有效并发为 5 时，分别验证同密钥及同用户不同密钥可同时准入 5 条；第 6 条因用户并发上限返回 429。单独验证钱包预留不足和真实预算耗尽仍拒绝 |
| 2 | 部分用量记录 | Messages 首包输入 100、缓存读取 20 后断流，记录总输入 120、缓存读取 20、输出未知；终结回执到达后不重复累计；错误后下次请求可继续 |
| 3 | 撤回断开后收尾 | 客户端取消立即传递给上游；准备期间取消后不发起推理，断开后释放租约和预留，后续请求可以执行 |
| 4 | 集成与恢复 | Chat/Responses/Messages 原生及转换入口、流式及上游流缓冲路径；并发故障、写失败、重启重放均只结算一次；200/429/200/502/200 持续通过 |

额外约束：
- 回归明确区分已知零、部分已知和完全未知；已有累计账本不得被重算。
- 持久化失败仍走原错误处理，不借本次修改绕过账本故障。
- 并发相关测试运行 race，使用取消/断流夹具检查没有遗留 goroutine 或在途占位。
- 完成代码后再构建本地升级包，基于当前实际部署源码保留 OfficeAce 等已部署能力，健康检查和关键调用链均通过才交付。


## 实现与回归入口

- 密钥、用户和订阅共用原准入事务。`TestMeteredKeyUserAndSubscriptionRespectFiveSlots` 对同一把密钥和共享用户的两把密钥分别保持 5 条在途请求，再检查第 6 条返回的并发错误。 `TestMeteredParallelHTTPCallsRespectUserSlots` 通过 Chat Completions 入口让 5 条请求同时停在测试上游，第 6 条收到 HTTP 429。上游放行后，5 条请求均返回 200，共结算 5 次。
- `TokenUsage.Partial` 保存当前尝试已报告的计数，缺省为 false。原日志只在请求级标记不完整，无法在重试后识别哪次尝试需要按已知部分计价。`TestNativePartialReceiptSurvivesRetry` 验证首包、后续累计快照和跨尝试汇总，`TestPartialUsageReplayKeepsMeasuredCostOnce` 验证账本恢复。
- 第三项已撤回。平台流式与缓冲入口沿用原请求取消信号；摘要恢复 `gateway.RunSummary` 调用，应用关闭恢复原有 HTTP 优雅关闭流程。
- `TestStreamPreparationHonorsClientCancellation` 验证流式和缓冲入口在准备期间取消后不发起推理；既有 `TestHTTPInterruptionReleasesAndRecovers` 与 `TestHTTPInterruptionBeforeHeadersReleasesAndRecovers` 验证 HTTP 断开、租约释放及后续请求。`TestUpstreamFailuresDoNotFreezeUser` 继续覆盖上游 429、502 之后的下一次成功请求。
