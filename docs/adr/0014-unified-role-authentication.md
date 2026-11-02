# 0014：统一账号登录与服务端角色授权

状态：采用，2026-11-02。

## 决定

参考 Sub2API 的统一登录、服务端角色、按角色选择菜单和路由的方式。`/login` 只有用户名和密码；`ConsoleApplication` 先查询 `/auth/session`，确认管理员后才挂载原生管理控制器，普通用户只挂载响应式用户页面。没有角色选择器或管理/个人空间切换链接。

API 客户端生命周期也只由 ConsoleApplication 管理：身份变化时先取消旧读取，再挂载新页面。子组件不重复 configure/dispose，避免子组件首次查询被父组件初始化取消；原生管理 DOM 的所有权保持原约定。

保留现有内置管理员：用户名 `admin` 已被用户命名规则保留，使用原 `Settings.AdminPassword` 哈希，服务器身份为 `builtin:admin`。不将其变成消费者、不接收用户分组、不转移旧密钥归属。普通用户持久化结构保持不变；`User.View()` 固定返回 user 角色。Role 仅是服务端会话及响应字段，登录、资料、用户更新请求均不接收角色。

统一认证由 access.Users 持有一个有界令牌注册表，app 注入当前管理密码的只读查询函数。HTTP 认证集中在 server/authentication.go；管理审计留在 server/audit.go。每次管理请求必须验证 admin 角色；普通用户接口验证 user 角色并沿用原资源归属检查。匿名为 401、角色不匹配为 403，前端隐藏菜单不能代替授权。

## 删除的旧路径

`/admin/auth/*` 和 `/user/auth/*` 不注册，也不提供兼容重定向。统一使用 `/auth/login`、`/auth/logout`、`/auth/session`；邮件恢复、OAuth 选项及回调同样位于 `/auth`。第三方应用回调需更新为 `/auth/oauth/callback`。

删除独立 AdminSessions、X-Admin-Password/Bearer 密码认证、本机自动解锁及 HTML capability 注入。移除对应 CLI 开关和密码清空按钮，后端拒绝清空管理员密码。管理业务接口仍存在，统一在后端校验角色。推理 API Key 继续只用于 `/v1`。

## 会话与兼容

只发放一个 `codearts_session` Cookie，Path=/、HttpOnly、SameSite=Strict，有效期八小时。TLS 或已配置 HTTPS 公开地址时带 Secure。令牌仅保存摘要；改密、用户停用/revision 变化、退出和重启使相应会话失效。一次密码登录替换并撤销当前浏览器之前的统一会话。

复用原用户登录的两个密码计算槽和每来源失败限流；所有角色每身份最多 16 个会话、全站最多 2048 个。密码计算不在 Store 事务锁或推理路径中。OAuth 创建会话仍必须从持久化消费者记录取身份，不能通过这个入口创建管理员会话。

升级需要重新登录，旧管理脚本需先调用统一登录并保存 Cookie。账号凭据、用户、分组、密钥、额度和用量不迁移，状态仍为 schema 10。模型转发、调度、重试、SSE 及三个上游适配器不增加角色认证步骤。

## 验证边界

验证统一登录、角色路由、旧接口和旧认证方式拒绝、伪造角色/跨用户资源拒绝、Cookie 属性、退出/改密/停用撤销、会话容量和并发，以及三平台既有业务回归。真实 SMTP 和 OAuth 授权仍需要实际应用配置；本批以本地协议测试验证回调路径和消费者会话约束。

参考：[Sub2API auth store](https://github.com/Wei-Shaw/sub2api/blob/main/frontend/src/stores/auth.ts)、[router](https://github.com/Wei-Shaw/sub2api/blob/main/frontend/src/router/index.ts)。沿用本项目的不透明 Cookie 会话，没有引入 Sub2API 的全部账户与令牌基础设施。
