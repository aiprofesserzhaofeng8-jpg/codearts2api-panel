# WorkBuddy 原生适配来源

协议端点、凭据字段、地区选择和请求头参考：

- https://github.com/linguo2625469/workbuddy2api-panel
- 本机参考版本：`73a7560efc7a88ebd9865db4c71ae7f6d85859f8`
- 主要参考文件：`internal/auth/auth.go`、`internal/panel/login.go`、`internal/panel/login_staged.go`、`internal/upstream/client.go`、`internal/upstream/headers.go`。

本目录是适配本项目 provider 接口的新实现，仅依赖标准库；没有引入原项目的 Redis、活动任务调度或指纹内容清洗。保留原项目的 MIT 许可和版权声明，见根目录 `LICENSE.workbuddy`。

现阶段连接 `/v3/config` 模型目录、`/v2/chat/completions` 推理、设备授权、令牌续期和个人积分包查询。企业额度、成长任务、签到等活动接口未接入。没有 WorkBuddy 账号实测之前，不能将模拟测试和授权入口连通等同于真实模型调用验收。
