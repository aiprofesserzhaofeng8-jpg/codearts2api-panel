# 成长任务真实流程复测

2026-10-30，运行版本为 `c921474`（文档 HEAD 为 `d8ab403`），已核对运行二进制与磁盘构建一致。本轮只测试现有流程，未改功能代码、配置或执行检查点。

结论：模拟上游的正向与恢复用例通过，当前真实账号仍无法完成领养和领奖。

| 步骤 | 真实结果 |
| --- | --- |
| 查询任务、协议及 Buddy 档案 | 成功；19 项任务，协议已同意，尚未领养 |
| 页面入口 | Buddy 不显示接取按钮，领养入口可用 |
| 点击一次「解锁并领养」 | 1 个执行请求，约 4.798 秒返回 HTTP 200、`state=pending`；不能当作成功 |
| 前置对话 | 上次超时检查点仍为待确认，本轮未重发模型请求 |
| 领养 | 未由上游确认，`owned=false` |
| 后续对话、领奖 | 未继续；没有奖励确认 |

新增模型调用为 0，测试前后余额均为 2198。设置、密钥、分组、账号开关/并发/健康/最近使用及每日签到字段不变，在途租约为 0，独立执行检查点原样保留。没有调用 CodeArts、Trae 模型。

针对性 `go test -race ./internal/server ./internal/workbuddy -run 'TestGrowthBuddy|TestGrowthActionsRequireRealChatAndCorrectWire' -count=1` 通过，覆盖模拟成功领养及后续对话、协议确认、损坏/未知状态、同账号互斥、跨重启恢复和平台隔离。它不能替代真实成功领取的验证。

实际流程目前卡在「已有不确定的前置对话记录 → 恢复只核对/尝试领养 → 仍未查到 Buddy」，真实解锁和奖励闭环未通过。当前接口只有通用的领养待确认说明，本轮没有捕获领养 POST 的原始业务拒绝，不能据此断言其具体业务原因，也不能宣称已定位前次响应头超时的底层原因。

证据：[真实操作](evidence/growth-flow-retest-2026-10-30/live.json)、[初始状态](evidence/growth-flow-retest-2026-10-30/preflight.json)、[状态不变量](evidence/growth-flow-retest-2026-10-30/invariants.json)、[针对性 race](evidence/growth-flow-retest-2026-10-30/race.log)。原始账号响应、检查点备份和页面截图留在本机私有、Git 忽略的验收目录。
