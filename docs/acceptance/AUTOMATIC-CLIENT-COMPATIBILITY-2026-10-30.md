# 自动客户端兼容与关键节点计时验收 · 2026-10-30

已部署到本机 `codearts2api-panel.service`。本轮修复启动参数与模型推荐配置，补齐入口拒绝日志、关键节点计时和安全的网络错误分类。用户使用项目统一启动器时，不需要手动设置兼容开关或环境变量。

对照版本为 `04472c1837ead846a39d506b795e86e55d5e786f`。部署前已备份旧程序和状态；部署后程序 SHA256 为 `fa16e1f91ba651e77ef2c2eda4c0e1473e96f5d44c6569c1ea1aecaeb08bce91`，运行 PID 3598536，运行程序与构建产物一致。前一轮只读诊断见 [客户端问题报告](CLIENT-COMPATIBILITY-AUDIT-2026-10-30.md)。

## 实际改动

| 问题 | 实施方式与边界 |
| --- | --- |
| Claude Code 默认 adaptive / effort 导致入口 400 | profile 统一声明当前支持的控制能力；启动器只为本次 Claude 子进程自动设置 `CLAUDE_CODE_EFFORT_LEVEL=auto`、`MAX_THINKING_TOKENS=0`。本机版本抓包证实主请求与后台标题请求均不再携带不支持的控制字段。没有修改全局客户端设置 |
| Trae Flash profile 返回 422 | 在统一模型策略中提供 `trae/cn/deepseek-v4.1-flash` 的保守建议：上下文 65536、输出 4096；服务端摘要保护关闭。三个启动器读取同一个 profile；其他缺少推荐配置、也缺少目录容量的模型仍返回 422 |
| 三个推理协议入口拒绝没有日志 | Chat、Messages、Responses 的解析、输入、模型和参数校验失败各记录一次，归类为 `invalid_request`，上游尝试为 0。不计为账号故障，不消耗或污染密钥预算，不伪造用量 |
| 无法区分首段等待和生成耗时 | 新增可选 `first_token_ms`，以及每次尝试的 `duration_ms`、`first_token_ms`、`status`、`error_kind`。旧日志兼容，缺少时间显示为横线；日志表展示首段 / 总耗时，悬停可查看尝试信息 |
| 网络错误只剩 Go 类型，无法定位 | WorkBuddy、Trae 的网络错误保留内部 cause，公开文字仅输出安全分类，如 `response_header_timeout`、`dns_error`、`connection_refused`。不把 URL、代理凭据或任意错误原文放入公开网络错误消息 |

这里的“自动兼容”指项目启动器自动选择当前支持的客户端请求方式。它不等于实现完整 adaptive / budget / effort 原生映射，也不保证上游关闭思考。绕过启动器、直接要求不支持的控制仍明确返回 400，不静默丢弃用户意图。

计时沿用现有响应解析，将原先独立的 finish 与 usage 两次解码合为一次。只在尚未收到首次有效增量时读取 delta；正文、推理、拒答及工具信息均可触发首段计时，角色或空片段不触发。非流式请求不假造首段时间。首段时间描述网关收到规范化上游增量的时间，不是客户端屏幕实际显示时间；尝试状态使用网关映射后的状态码。仍在请求结束时统一记录，无逐片写日志、写盘或新增全文缓冲。

## 真实客户端结果

本机 Pi 1.4.0、Claude Code 2.1.238；统一启动器的构造逻辑，正式网关 profile，无测试参数覆盖。工具任务只读取临时目录中未知的随机文件内容，同时验证工具真实执行、结果回填和最终答案完全一致。

| 上游 | Pi | Claude Code | 到达网关的推理请求 |
| --- | --- | --- | --- |
| CodeArts | 通过，4.019 秒 | 通过，6.239 秒 | 2 + 3 |
| WorkBuddy | 通过，3.866 秒 | 通过，4.836 秒 | 2 + 3 |
| Trae | 通过，3.223 秒 | 通过，4.124 秒 | 2 + 3 |

6/6 工具闭环通过；15 次推理请求、15 次上游尝试、15 条日志，全部 HTTP 200，用量均有回执。Claude 的 3 次请求包含后台标题任务；不能将其与工具回合混为一谈。每条日志的请求 ID 均能匹配实际代理记录，首段时间不大于总耗时，尝试时间、状态均完整。没有通过删改答案或工具参数来修正客户端输出。

三个上游 × 三个启动器的正式 `--check` 共 9/9 通过。另向部署服务发送三个协议的非法请求，3/3 均产生且只产生一条入口拒绝日志，上游尝试为 0，临时密钥的请求 / Token / USD 预算和未知用量标记均保持初始值。

WorkBuddy 本轮 5 次真实请求均正常，网关总耗时 1.545–1.966 秒。之前的 30 秒连接失败和 HTTP 500 没有重现，因此不能断言历史慢请求已经彻底消除或由网关代码导致。本轮先补可定位的计时及网络分类，保持原有超时、重试和熔断策略。

## 性能与回归

相同基准脚本在对照版本和修改版本分别运行，`-cpu=1 -benchtime=250ms -count=5`，下表为中位数；上游为受控响应，数值不包含真实网络和模型排队。

| 受测路径 | 对照版本 | 修改版本 |
| --- | --- | --- |
| 完整流式处理，100 个有效分片 | 0.373 ms | 0.339 ms |
| 非流式，1 KiB 正文 | 0.0248 ms | 0.0232 ms |
| 非流式，1 MiB 正文 | 2.811 ms | 1.995 ms |
| 非流式，8 MiB 正文 | 21.954 ms | 15.534 ms |

完整流式基准每请求分配次数从 1197 降至 1100，分配字节从 142363 增至 142906，增加约 0.4%。受控真实 socket 并发 1 / 4 / 16、流式 / 非流式分别跑三轮：前后各 378 个请求均通过，无残余租约；修改版本各组 p95 中位数为 4.321–12.878 ms，未观察到明显性能退化。不能据此保证所有生产负载零开销。

验证完成：全量 `go test ./... -race -count=1`、`go vet ./...`、构建、启动器 9 项单元测试、浏览器明暗 / 移动布局与新旧日志兼容、用量视图回归。取消、超时、跨协议内容与工具保真沿用完整回归；本轮没有重新宣称所有客户端长程压缩或所有模型已验收。

## Codex 仍受宿主权限限制

Codex CLI 0.165.0 的配置检查通过，但真实原生沙箱预检仍失败，并在模型调用前自动停止。本轮将一次 `/usr/bin/bwrap` 调用与同时间、对应进程的内核日志关联，确认 `unprivileged_userns` AppArmor profile 拒绝了 `net_admin`，并出现 `setpcap` 拒绝；因此无法配置沙箱 loopback。

这是已定位、尚未实施的系统修复。项目已有校验过的发行版 bwrap profile 安装脚本，加载系统策略需要管理员权限，当前进程没有该权限。没有关闭 AppArmor 或 Codex 隔离来制造通过结果。依据 [Codex 官方权限说明](https://learn.chatgpt.com/docs/permissions)，也没有把受限文件系统策略切换到旧 Landlock 作为绕过方式。

## 参考、证据与收尾

参考固定的 Sub2API 提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2`：[协议边界参数适配](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/pkg/apicompat/anthropic_to_responses.go)、[可选首 Token 与总耗时字段](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/ent/schema/usage_log.go)。本项目按三个上游的已验证能力配置客户端，未照搬另一种上游协议的 effort 转换。

Claude 配置语义参考 [官方环境变量说明](https://code.claude.com/docs/en/env-vars) 和 [省略 effort 的方式](https://code.claude.com/docs/en/model-config#send-no-effort-level)，以本机实际请求和工具结果验证。新增行为及使用边界见 [客户端说明](../CLIENTS.md)。

结束时健康 HTTP 200、12/12 账号 ready、在途 0、临时密钥全部删除，设置与部署前一致。新增推荐策略来自代码默认值，既有管理员模型策略仍优先。真实测试产生的请求日志和用量按正常规则保留。

脱敏证据：[总览](evidence/automatic-client-compatibility-2026-10-30/summary.json)、[真实客户端](evidence/automatic-client-compatibility-2026-10-30/live-clients.json)、[网关日志](evidence/automatic-client-compatibility-2026-10-30/gateway-logs.json)、[计时核对](evidence/automatic-client-compatibility-2026-10-30/timing-verification.json)、[入口拒绝与启动检查](evidence/automatic-client-compatibility-2026-10-30/checks.json)、[性能对照](evidence/automatic-client-compatibility-2026-10-30/benchmark-summary.json)、[并发对照](evidence/automatic-client-compatibility-2026-10-30/capacity-summary.json)、[宿主拒绝证据](evidence/automatic-client-compatibility-2026-10-30/codex-host-correlation.json)、[结束状态](evidence/automatic-client-compatibility-2026-10-30/verification.json)。
