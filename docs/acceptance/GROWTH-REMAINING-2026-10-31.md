# WorkBuddy 剩余成长任务验收

2026-10-31，当前实际账号 **19 项任务中 18 项已领取**。本轮新增领取 **16 项、1400 积分、80 能量**；余额 **2598 → 3998**。只剩 `wb_wechat_oa_subscribe_task`，原生进度 0/1，需本人在微信关注官方公众号满 24 小时。该任务没有伪造关注事件或启用自动完成按钮。

实现提交 `3ac48d2842242c77fcb04e42a2ad2ee8471d836d`，已部署本机用户服务，未推送远端。

## 问题根因与维护边界

前一轮只修复了 `first_buddy` 和 `chat_5`。其他未达标任务的领取按钮禁用符合上游进度，但缺少对应的自动执行器，不能靠重复点领奖解决。参考项目的执行链分别属于 billing 活跃、copilot 桌面事件、WorkBuddy Web 事件，以及需真实模型响应的专家/技能任务，不能混成一个通用上报请求。

新增的四项 `expert_use_*` 任务，其显示名称与官方市场专家名称并不完全相同。例如任务写“中国电商运营专家”，指定条目名为“卖得好”。本次保留原生 `jump_url`，只解析官方 WorkBuddy 链接中的 `expertId`，再与真实市场条目核对；不访问任意跳转 URL，不用相似名称猜专家。没有跳转 ID 的轻量云/无畏契约任务采用精确市场名称匹配。

专家和技能沿用参考项目的任务专用 `fast-model`。它不在当前公共模型目录里，原先统一的目录检查会错误阻止执行；现在只有提供方本地白名单声明的成长动作可使用此原生别名，普通推理路由的模型解析与 404 行为不变。本轮实际确认该别名可用。

代码按职责拆分：`growth_catalog.go` 管理白名单及上限；`growth_desktop.go` / `growth_desktop_events.go` 管理独立桌面/Web 请求和事件；`growth_experts.go` 管理专家资源与使用报告；`growth_desktop_chat.go` 仅处理任务专用的完整短对话。服务端继续复用账号锁、双执行名额、原子付费检查点和用量记账。没有在正常推理、SSE 转发、CodeArts 或 Trae 路径加入任务逻辑。

参考版本固定为 [workbuddy2api-panel 73a7560](https://github.com/linguo2625469/workbuddy2api-panel/tree/73a7560efc7a88ebd9865db4c71ae7f6d85859f8) 的 `internal/upstream/desktop.go` 和 `internal/panel/autotask.go`，原 MIT 许可说明保留在 `LICENSE.workbuddy`。当前官方任务表已有该参考版本未列出的指定专家和公众号任务；不能照抄其旧的 17/18 总数。

## 逐项真实结果

所有下列动作均通过当前管理页面的单项执行入口完成。原生任务回读为已领取，并再次单独查询了最终余额。

| 任务 | 本轮真实模型调用 | 积分 / 能量 | 最终状态 |
| --- | ---: | ---: | --- |
| `playbook_prompt` | 0 | 100 / 5 | claimed |
| `RichMeow_Chat` | 0 | 100 / 5 | claimed |
| `Library_read` | 0 | 100 / 5 | claimed |
| `Hp_Appearance` | 0 | 100 / 5 | claimed |
| `Buddy_App` | 0 | 100 / 5 | claimed |
| `template_5` | 0 | 100 / 5 | claimed |
| `automation_1` | 0 | 100 / 5 | claimed |
| `Expert_lighthouse` | 1 | 100 / 5 | claimed |
| `WorkBuddy_Valorant` | 1 | 100 / 5 | claimed |
| `expert_use_maidehao` | 1 | 50 / 5 | claimed |
| `expert_use_douyinyin` | 1 | 50 / 5 | claimed |
| `expert_use_shangjia_xuanchuan` | 1 | 50 / 5 | claimed |
| `expert_use_bowangwang` | 1 | 50 / 5 | claimed |
| `Expert_team_use_3` | 3 | 100 / 5 | claimed |
| `skill_1` | 1 | 100 / 5 | claimed |
| `expert_5` | 0 | 100 / 5 | claimed |

`playbook_prompt` 的对话事件链同时满足了 `RichMeow_Chat`，后者只领取，没有重复上报；前面六项指定专家的实际使用也满足了 `expert_5`，后者只领取，没有再调用五次模型。每次执行和领奖前都会重读实际状态，因此同一行为满足多个任务时可直接复用上游计分结果。

10 次真实调用全部 HTTP 200，每次尝试 1 次且用量已知：总输入 **380 Token**，输出 **48 Token**；持久化统计和调用日志均增加 10 条，与界面动作一致。余额本次净增 1400 与确认奖励总额一致，这一快照不宣称任务模型未来永远免费。API 密钥预算未参与这些管理动作。事件链不代表实际创建了客户端资源：自动化任务只按参考项目上报事件，不创建后台定时任务；主题任务实际设置了官方账号的和平精英主题。

## 隔离、回归与运行验证

- 全量 `go test -race ./...` 通过，服务端约 291 秒；最终任务专用别名修正后重新执行成长相关服务端及 WorkBuddy 竞态测试，均通过。`go vet ./...`、构建、JavaScript 语法及 diff 检查通过。
- 六组 Playwright 回归通过，包含原有五组及新增的混合任务顺序/公众号指引检查；事件任务先于付费任务，同账号串行、最多两账号并行、取消与错误保护仍有效。
- 当前页面显示 19 项任务、18 项已领取；自动执行与领取按钮在没有剩余可执行/可领奖励时禁用，公众号只显示本人关注满 24 小时的指引。真实页面无 JavaScript 错误，临时管理会话已退出。
- 对比部署前状态：全部账号凭据、每日领奖字段、健康、冷却、设置、分组和客户端密钥预算保持一致。CodeArts、Trae 账号的全部持久化字段未变化；WorkBuddy 只因真实对话更新最近使用时间。历史未确认付费检查点原样保留，新付费步骤记录独立保存。
- 最终服务 PID `13920`，运行二进制与候选构建 SHA-256 `86cc0d3232ed5f3472d020e4f7a3819d6a304bb4ee31a3200c57c6c14f4da45e` 一致；工作目录及实际提供的 JS/CSS 与源码一致。健康为 `ok`，12 账号 / 12 可用 / 83 模型，在途请求为 0。

没有为本次修改额外调用 CodeArts/Trae 的真实付费模型；它们的验证范围是完整回归和运行状态对比。1.05 秒事件间隔、专家间隔和异步计分回读仅在显式成长执行中发生，普通请求没有新增等待。结果未知时本轮不重发、不绕过旧付费检查点。

可复查 [汇总检查](evidence/growth-remaining-2026-10-31/final-checks.json)、[真实动作与原生状态](evidence/growth-remaining-2026-10-31/live.json)、[页面回归](evidence/growth-remaining-2026-10-31/browser-regressions.json)。凭据、账号标识、原始响应、截图和备份仅保留在被忽略的本机 `artifacts/` 目录中。
