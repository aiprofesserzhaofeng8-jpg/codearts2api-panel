**客户端兼容与计时诊断 · 2026-10-30**

本轮为诊断，不实施业务修复。运行源码提交为 `04472c1837ead846a39d506b795e86e55d5e786f`，服务 PID 2781919，程序 SHA256 为 `08ce2f9deaf028a7c19b9e5a7791468d3b494f15efd74d9ecdc47c162259ece3`，与本地程序一致。没有修改网关业务代码、部署程序、客户端正式配置或账号策略，没有重启服务。

使用本机 Claude Code 2.1.238、Codex CLI 0.165.0、Pi 1.4.0。先以 localhost 接收端捕获真实 CLI 请求，再通过临时代理原样转发到运行中的网关及真实上游。工具任务只读取临时目录中的随机 `probe.txt`，验收同时检查实际工具执行、工具结果回填以及最终答案中的随机字符串；不以 HTTP 200、进程返回 0 或模型说“已读取”判定工具成功。

**实际结果**

| 上游 | Pi 默认请求 | Codex 默认请求 | Claude Code 默认请求 | Claude Code 临时兼容配置对照 |
|---|---|---|---|---|
| CodeArts | 工具闭环通过 | 本机沙箱阻断 | 参数校验 HTTP 400 | 工具闭环通过 |
| WorkBuddy | 工具闭环通过 | 本机沙箱阻断 | 参数校验 HTTP 400 | 被连接失败、上游 HTTP 500 及客户端重试打断，未完成验收 |
| Trae | 临时测试配置下工具闭环通过 | 临时测试配置下，本机沙箱阻断 | 临时测试配置下，参数校验 HTTP 400 | 临时测试配置下工具闭环通过 |

Trae 当前 `/v1/client-profile` 返回 422，所以表中的 Trae 协议测试使用了隔离的临时推荐参数覆盖：只在测试进程中指定 Trae 模型、关闭摘要扩展，并限制输出参数。没有将 WorkBuddy 配置写入 Trae 正式配置，也没有据此声称 Trae 所有模型或上下文容量已验证。

**问题、原因与最小建议**

| 编号 | 分类 / 影响 | 证据与原因 | 建议 |
|---|---|---|---|
| C01 | 已确认的客户端参数兼容障碍，优先处理 | Claude Code 主对话默认发送 `thinking: {"type":"adaptive"}` 和 `output_config: {"effort":"high"}`；后台标题请求也带 effort。三个上游共 6 个实际请求被网关返回 400。`applyAnthropicThinking` 在调用账号前拒绝 effort/adaptive，这不是上游拒绝、凭据失效或模型没有权限 | 优先考虑启动器中的显式兼容配置。临时进程设置 `CLAUDE_CODE_EFFORT_LEVEL=auto`、`MAX_THINKING_TOKENS=0` 后，捕获到的主请求不再携带上述控制字段，CodeArts、Trae 真实工具闭环通过。WorkBuddy 对照仍待上游恢复后的补验。完整原生思考强度适配暂不扩张 |
| C02 | 已确认的本机运行环境障碍 | Codex 发起了工具调用，但只读执行沙箱报 `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted`。不经过网关、不调用模型的 `codex sandbox` 和系统 `/usr/bin/bwrap` 均复现；`unshare` 写 uid_map 也被拒绝 | 单独排查宿主的用户 / 网络命名空间权限。当前证据不能确定具体由哪一项宿主策略导致，不直接断言为 AppArmor。保留沙箱预检，不通过关闭隔离或修改网关协议来绕过 |
| C03 | 已确认的配置缺口，不是 Trae 模型 API 故障 | Trae Flash 缺少经配置的客户端推荐参数，profile API 返回 422，三个统一启动器 `--check` 都在启动前退出；隔离参数下 Pi、Claude Code 已能完成工具任务 | 在统一模型策略中补一份经验证的 Trae 推荐配置，避免在三个启动器里各自硬编码。422 是现有保守设计，配置补齐即可，无需放开所有未知模型 |
| C04 | 已确认的入口拒绝日志遗漏 | 默认轮临时代理收到 26 个推理请求：20 个 200、6 个 400；同一测试密钥的网关日志只有 20 条 200。Messages 参数校验提前返回，未执行 `s.record` | 记录入口校验失败，并明确 `attempts=0`、未调用上游。每请求结束记录一次，避免重复统计，不能把入口拒绝记成账号失败或已消耗的上游用量 |
| C05 | 已观察到的上游路径故障和慢请求，根因未定 | WorkBuddy 对照的两个并发请求约 30 秒后连接失败；一次后续调用返回 HTTP 500 / 业务码 10000。客户端继续重试，出现无可用账号的 503。单独恢复探测最终 HTTP 200、内容正确，但耗时 31.684 秒；网关记录只有 1 次尝试 | 先观测并定位连接、等待响应头、生成等阶段。现有证据不足以认定是网关解析变慢、上游模型排队、代理网络或凭据问题，不据此调整超时、熔断或账号调度策略 |
| C06 | 已确认的计时能力缺口，属于增强项 | `LogEntry` 只有总 `latency_ms`；`RequestAttempt` 没有耗时、状态及失败分类；缺少首次有效输出时间。因此 31.7 秒单次尝试无法仅凭当前日志拆解 | 仅在请求开始、上游尝试开始 / 结束、首次有效输出和请求结束计时；结束时统一写一条日志。正文、推理和工具参数均应视为有效输出，不能把角色或空事件当首字。沿用已有解析，不新增逐片完整解析、写盘或全文缓冲；真正实施后再做性能对照 |

兼容配置的含义是“客户端不请求 adaptive / effort 控制，保留网关和上游原有默认行为”。它不能证明上游思考已关闭，也不能冒充完整 enabled / adaptive / budget 映射。如果用户需要显式控制思考强度，仍需按 CodeArts、WorkBuddy、Trae 的实际原生能力分别实现并验证。

Codex 的默认工具测试通过启动器的 `build_launch` 构造协议配置，没有执行启动器主入口的沙箱预检；因此可以观察网关和上游的工具往返，以及本机工具执行失败。另行运行了正式启动器 `--check-sandbox`，它正确在模型调用前阻止启动。不能将该结果解读为生产启动器缺少保护。Codex 的模型元数据回退警告也被观察到，但没有证据表明它是本轮工具失败的原因。

**Sub2API 的参考边界**

参考固定提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2`，不声称为其最新版本。其 [Anthropic → Responses 转换](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/pkg/apicompat/anthropic_to_responses.go) 在协议边界明确转换思考偏好；本项目三个原生上游不能直接套用同一个 OpenAI effort 字段。其 [用量日志结构](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/ent/schema/usage_log.go) 单独保存 `first_token_ms`、`duration_ms` 等字段，可以借鉴这种计时粒度。

Claude Code 的 [官方环境变量说明](https://code.claude.com/docs/en/env-vars) 和 [模型配置说明](https://code.claude.com/docs/en/model-config#send-no-effort-level)提供省略 effort、关闭客户端 thinking 请求的配置方式；本轮以本机版本实际抓包和真实工具闭环为准。Codex 自定义 provider 继续使用官方支持的 [Responses 配置](https://learn.chatgpt.com/docs/config-file/config-reference)，没有改成不支持的 Chat wire_api。

**验证范围、消耗与收尾**

- 实际 CLI 默认轮 9 例；临时兼容配置对照 3 例；另有一次 CodeArts 对照在诊断结果解析器遇到 `api_error_status=null` 后中断，清理正常执行。修正的只是 `/tmp` 诊断脚本，随后重跑对照；那次产生的 3 个实际请求计入总数。
- 累计 42 个请求到达网关推理入口，记录 36 条日志，33 次上游尝试：30 条 HTTP 200、3 条上游尝试失败的 502、3 条未选到账号的 503；另有 6 条入口 400 未被现有日志记录。代理为阻止 WorkBuddy 客户端继续重试，另在本地拒绝了 2 次请求并返回 429；这 2 次没有到达网关，不能称作上游限流。
- WorkBuddy 对照被主动停止；没有把未完成、受环境阻断或上游故障的案例写成通过。故障请求没有最终用量时不能推算为零消耗，也不把 Token 回执当作完整积分账单。
- 临时代理保存合成测试响应以便核对，其外部计时只用于诊断，不作为网关新增计时实现或性能基准。没有实施流式缓冲、计时、日志或参数映射改造，所以本轮也没有声称验证了这些未来改动的性能。
- 结束检查：临时测试密钥全部删除，健康 HTTP 200、在途 0、账号仍为 12 个，CodeArts / WorkBuddy / Trae 账号可用；WorkBuddy 恢复请求后状态 ok、失败计数 0、无活动模型冷却。设置摘要与测试前一致，程序摘要未变。
- 尚未重新验收真实 CLI 的主动取消、长会话压缩、所有型号或压力场景；本轮默认请求已经阻断的客户端不能宣称完成整套客户端验收。

建议先处理 C01 的启动兼容配置和 C03 的模型策略配置；C02 在运行环境独立处理。C04 与 C06 可以作为一批小日志改动评审。C05 先补证据，不立即改超时与调度。以上建议尚未实施。

证据目录：[脱敏诊断材料](evidence/client-compatibility-audit-2026-10-30/)。关键文件为 [默认请求捕获](evidence/client-compatibility-audit-2026-10-30/capture-summary.json)、[兼容配置请求捕获](evidence/client-compatibility-audit-2026-10-30/capture-alternatives.json)、[默认真实 CLI](evidence/client-compatibility-audit-2026-10-30/live-summary.json)、[兼容对照](evidence/client-compatibility-audit-2026-10-30/compatibility-summary.json)、[日志核对](evidence/client-compatibility-audit-2026-10-30/all-audit-server-logs.json)、[启动器检查](evidence/client-compatibility-audit-2026-10-30/launcher-checks.json)、[恢复请求](evidence/client-compatibility-audit-2026-10-30/workbuddy-recovery.json)、[结束检查](evidence/client-compatibility-audit-2026-10-30/verification.json)。
