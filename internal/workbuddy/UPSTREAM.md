# WorkBuddy 原生适配来源

协议端点、凭据字段、地区选择和请求头参考：

- https://github.com/linguo2625469/workbuddy2api-panel
- 本机参考版本：`73a7560efc7a88ebd9865db4c71ae7f6d85859f8`
- 主要参考文件：`internal/auth/auth.go`、`internal/panel/login.go`、`internal/panel/login_staged.go`、`internal/upstream/client.go`、`internal/upstream/headers.go`。

本目录是适配本项目 provider 接口的新实现，仅依赖标准库；没有引入原项目的 Redis 或活动任务调度。搜索、系统提示词与指纹清洗已按当前网关架构接入，见 `docs/WORKBUDDY-FEATURES.md`。保留原项目的 MIT 许可和版权声明，见根目录 `LICENSE.workbuddy`。

现阶段连接 `/v3/config` 模型目录、`/v2/chat/completions` 推理、设备授权、令牌续期、个人积分包查询与每日签到。成长任务已接入国内个人账号的默认列表查询与达标奖励领取，见 `docs/WORKBUDDY-GROWTH.md`；任务自动执行、企业额度和其他活动接口未接入。2026-10-29 已实测个人账号签到成功并增加 100 积分；此结果不代替真实模型调用验收。

搜索参考 `internal/upstream/search.go`、`internal/server/messages.go`；提示词和清洗参考 `internal/prompt/*`、`internal/upstream/sanitize.go`。清洗仅作用于系统提示词中的明确模板，全部业务消息和工具参数原样保留；降级重试限定当前请求、同一账号且没有输出分片，不使用参考项目的全局降级窗口。

公共请求变换现位于 `internal/requestpolicy`，业务文本及错误码字符串不再参与清洗；各平台维护边界见 `docs/PROVIDER-FEATURES.md`。
