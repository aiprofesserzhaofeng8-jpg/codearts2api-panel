# WorkBuddy 原生适配来源

协议端点、凭据字段、地区选择和请求头参考：

- https://github.com/linguo2625469/workbuddy2api-panel
- 本机参考版本：`73a7560efc7a88ebd9865db4c71ae7f6d85859f8`
- 主要参考文件：`internal/auth/auth.go`、`internal/panel/login.go`、`internal/panel/login_staged.go`、`internal/upstream/client.go`、`internal/upstream/headers.go`。

本目录是适配本项目 provider 接口的新实现，仅依赖标准库；没有引入原项目的 Redis 或活动任务调度。搜索、系统提示词与指纹清洗已按当前网关架构接入，见 `docs/WORKBUDDY-FEATURES.md`。保留原项目的 MIT 许可和版权声明，见根目录 `LICENSE.workbuddy`。

现阶段连接 `/v3/config` 模型目录、`/v2/chat/completions` 推理、设备授权、令牌续期、个人积分包查询与每日签到。成长任务支持国内个人账号默认列表查询、接取、领养前置与依赖编排、达标奖励领取，以及桌面/Web 活动事件、真实模型/专家/技能任务执行，见 `docs/WORKBUDDY-GROWTH.md`。接取与对话报告协议参考 `internal/upstream/tasks.go`、`internal/upstream/report.go`；Buddy 档案/协议/领养参考 `internal/upstream/travel.go` 与 `internal/panel/autotask.go`。`first_buddy` 和 `chat_5` 使用参考项目的完整 `chat_request_send` 活跃事件，通过 billing 域发送，不调用模型；模型、专家、技能任务要求真实对话与响应标识，任务专用 fast-model 别名不加入公共模型列表。专家从市场读取真实条目，指定专家优先采用官方 jump_url 的 expertId，防止按相近名称选错。Buddy 维护请求使用独立 billing 请求头，普通推理不变。首次领养需要明确确认协议；历史付费检查点不阻塞活动任务，只有上游回读才确认领养和领奖。企业额度和其他活动接口未接入。2026-10-29 已实测个人账号签到成功并增加 100 积分；此结果不代替真实模型调用验收。

搜索参考 `internal/upstream/search.go`、`internal/server/messages.go`；提示词和清洗参考 `internal/prompt/*`、`internal/upstream/sanitize.go`。清洗仅作用于系统提示词中的明确模板，全部业务消息和工具参数原样保留；降级重试限定当前请求、同一账号且没有输出分片，不使用参考项目的全局降级窗口。

公共请求变换现位于 `internal/requestpolicy`，业务文本及错误码字符串不再参与清洗；各平台维护边界见 `docs/PROVIDER-FEATURES.md`。
