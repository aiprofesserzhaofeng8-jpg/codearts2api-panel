# 积分领取范围

2026-11-05 核对。这里的“已接入”表示项目实现范围，不等于每个账号都符合上游活动条件。实际奖励数量与是否可领以上游状态为准。

| 平台/奖励 | 当前处理方式 |
|---|---|
| CodeArts 运营福利 | 领取服务端确认可自动领取的已支持活动；旧免费 Token 通道已停用 |
| WorkBuddy 个人每日签到 | 已接入单号、批量和自动领取；企业账号不套用个人签到 |
| WorkBuddy 成长任务 | 已有独立任务流程，只有真实达到条件的奖励才能领取，不混入每日签到 |
| Trae CN 每日签到 | 已接入，先查状态、提交后复查；设备限流或账号条件不满足不记成功 |
| TeleAgent 每日积分 | 本批接入，先查可领状态、仅提交 daily_login、复查确认后刷新余额 |
| TeleAgent 首登/月度基础积分 | 官方客户端有单独赠送通知及协议条件；当前不代为调用无类型领取接口 |
| TeleAgent 邀请/其他活动 | 根据上游响应展示活动提示；须真实达到官方条件，当前不自动提交 |
| Qoder、Kiro、ChatGPT、Claude | 当前没有已接入且核实的每日积分领取协议，不提供通用“领积分”调用 |

## 使用

在账号池点击账号行的「每日签到」，或顶部「领取活动福利」批量处理支持的账号。「活动福利」中选择账号并刷新列表，可查看该账号的可领数量和活动提示；刷新列表是只读操作。

需要定时领取时，打开「配置 → 活动福利自动领取」，设置每日执行时刻。系统保留原有开关值，不因升级自动开启。外部平台按 UTC+8 日期记录本地去重，最终是否今日已领仍由上游确认；失败/未确认后等待 30 分钟。

TeleAgent 页面可能显示每天 100 积分，但项目不会硬编码这个数。状态接口必须返回明确的 `claimed_today`；缺字段、错误业务码、账号身份不一致或任务隐藏时，不会盲目提交。领取接口返回 200 也不单独作为到账依据。

## TeleAgent 的协议证据和边界

- v2 每日状态：`/superCowork/sapi/user/portal/companyStore/v2/checkDayTask`。
- 明确的每日领取：`/superCowork/sapi/user/portal/companyStore/checkTask?type=daily_login`。上游使用 GET，但它是写操作，只允许由领取动作触发。
- 显式导入独立门户凭据时使用 `/meter/super-agent/user/portal/` 下对应路径，令牌不会混发。
- 参考实现：[账号业务 API](https://github.com/anye-99/dsh-codearts-auth/blob/686658793c982cd0e1baedbcfa10a396ce5d1a09/src/teleagent-credits.ts)、[独立门户](https://github.com/anbulang/dsh-connect-teleagent/blob/14bb4bb16d6961addff63ced79866f9a74391108/lib/portal.js)。本机安装的 3.0.0-rc.4 renderer 也保留 v2/checkTask 路径常量，并使用 v3 活动展示及首登/月度通知。
- 当前只接入有明确前后状态的 daily_login。不会自动访问活动 URL，不会用无类型 `checkTask` 刷新列表，也不会跳过官方协议或伪造邀请/学生等资格。

本批未使用真实账号执行 TeleAgent 领取，当前服务端对 v2 接口及账号资格的实际响应仍待真实验收。模拟回归覆盖确认成功、已领、无资格、状态不全、限流、取消、错误重定向、并发去重、保存重开和余额刷新；失败不修改推理账号健康。
