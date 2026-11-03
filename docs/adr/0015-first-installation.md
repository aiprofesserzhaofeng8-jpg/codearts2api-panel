# ADR 0015：管理员首次安装向导

状态：已实施，2026-11-02。初始化凭证与固定登录名的设计已由 [ADR 0017](0017-email-administrator-setup.md) 取代；下文保留原始决策背景。

## 问题

原首次启动自动生成管理员密码，用户需先查找访问文件。现在改为页面设置初始密码；普通用户由管理员创建，不增加公开注册或请求端角色选择。已有部署必须保留密码、平台凭据、分组、密钥和账本。

## 参考与取舍

参考 Sub2API 的 [安装状态与接口](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/setup/handler.go)、[初始化与安装锁](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/setup/setup.go) 和 [安装向导](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/frontend/src/views/setup/SetupWizardView.vue) 的「检查是否安装 → 设置管理员 → 持久化 → 关闭初始化」流程，独立实现，没有复制第三方源码。

本项目是单实例 JSON 存储，无需 PostgreSQL/Redis 配置页；保留已有 `admin` 身份及密码哈希规则，不引入管理员表、邮箱登录或 schema 迁移。新增部署私有初始化凭证证明服务器控制权，避免把公网第一个访问者直接认作管理员。普通用户仍由管理界面创建并绑定分组。

## 职责

- `access.Installation`：恒定时间凭证比较、密码/确认校验、有界提交互斥。密码哈希仅在安装时执行；提交失败可重试。
- `storage`：在存储事务中同时复核空状态及文件不存在，原子保存密码哈希和默认调用密钥。所有权凭证为 `*.setup.json`、权限 0600，安装后删除；独立 `*.installed` 阻止误删快照后重新初始化。
- `server.NewSetupHandler`：只提供安装状态、安装写入和 SPA 资源；严格 JSON、同源校验、失败限流，不签发会话。普通认证、管理员、用户、模型和健康接口均返回 503。
- `app.RunInitialSetup`：临时监听实例。安装提交后先排空 HTTP，再启动原应用；等待安装期间没有平台实例、后台续期、领奖或周期保存。`ready` 仅由正式 HTTP 实例返回 true。
- `ConsoleApplication`：统一 API 客户端生命周期及启动路由。`SetupPage` 只负责三个步骤和安装后有界就绪轮询；提交回包丢失时先查询状态，不盲目重放。

## 完成与恢复

正式服务的 `/setup/status` 返回 `needs_setup=false, ready=true`；`/setup/install` 固定拒绝 403。密码、API Key、普通用户角色仍由原有授权链管理；每个普通请求不增加安装判断。

初始化等待期间重启复用私有凭证；提交后崩溃可根据已保存的密码状态恢复正式启动。安装标记即使丢失，已有状态仍拒绝重装。状态文件丢失但安装标记、初始凭据、恢复文件、成长状态或账本存在时，启动在 `storage.Open` 重放之前停止，要求恢复完整状态。全部数据被删除相当于新部署，不能把该机制当作服务器文件权限的替代。

已有部署沿用原密码并补充安装标记。显式 `CODEARTS_ADMIN_PASSWORD` 保留自动部署/恢复行为，兼容原 `*.access.json` 文件。旧部署已有业务数据而无密码时仍走原管理员恢复流程，不开放向导。

范围仍为单进程写状态；没有新增多实例安装协调、支付、公开注册或上游协议改动。
