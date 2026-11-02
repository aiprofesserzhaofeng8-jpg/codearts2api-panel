# ADR 0013：用户额度、兑换码、订阅及独立登录

状态：已实现；验收与真实外部配置限制见 [用户中心第二批报告](../acceptance/USER-CENTER-2-2026-11-02.md)。

## 业务范围

参考 Sub2API 的余额预留、实际结算、事务核销、订阅快照与独立用户登录规则，使用本项目的存储边界独立实现，不复制其数据库、Redis 或平台适配代码。未接支付、在线充值渠道、订单、退款和邀请返利。

参考源：

- https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/redeem_service.go
- https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/subscription_service.go
- https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/billing_inflight_reservation.go
- https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps
- https://wiki.linux.do/Community/LinuxDoConnect

## 所有权与兼容

access 持有纯权益值、十进制校验、用户会话及单次验证流程；identitytransport 仅处理 SMTP TLS 与 OAuth HTTP。app 注入传输接口，业务与三个推理平台相互独立。server 仅处理 Cookie、请求、响应和页面；gateway.Admission 协调预留。storage 唯一持有权益、原有锁、在途预留、原子提交与结算账本，不增加授权缓存。

schema 10 的 9→10 迁移只增加可选权益。旧用户余额限制关闭、无订阅，原有预算不变。旧二进制不能读 schema 10；不允许直接修改版本号回退，也不恢复旧凭据。

## 额度与结算

额度是按管理员模型 USD 参考价计量的内部额度，与上游积分、实际账单和支付资金独立。以 1e-8 USD 的整数单位记录，单次调整上限 100 万 USD，API 写入用十进制字符串，拒绝多余小数、NaN 和指数格式。用户查看本金、在途预留和流水；管理调整保留原因，以钱包 revision CAS 防止网络重试重复发放。

默认用户继续走原有准入路径。启用余额时，在真实型号解析后、执行上游前预留；估算使用已有请求体字节数作为输入 Token 保守估算、8192 输出 Token。重试复用同一请求预留，加上前次消耗，不能无限累加重试预算。此为软额度：多模态和超长实际输出可能高于估算；实际结算不截断有效返回，可形成负余额，后续请求停止。没有用量或价格时不猜测零费用，保留已知消耗并暂停余额调用，管理员核对后可解除。

结算沿用 fsync 用量 journal，费用和待核对标志跟随历史价格一起持久化；重启按序号恢复，不重新按当前价格计费。取消、无上游尝试的拒绝和处理结束均幂等释放预留；已产生真实上游消耗的失败仍按回执结算。用户在途时不能调整钱包、兑换或切换订阅，避免旧请求落在新权益上。流水保留全站最近 10000 条，余额和已用预算不依赖展示历史。

## 订阅与兑换

一个用户保留一个生效分组、一个生效订阅。套餐包含分组、并发上限、有效天数、请求/Token/USD 额度和 lifetime/day/week/month 周期；周期边界为 UTC，周一开始。同套餐未过期续期延长到期日且保留快照和已用额度；模板修改不追溯已有订阅。过期后重新分配开启新订阅。到期与停用即时阻止新调用，不依赖后台定时器；管理员显式移除订阅后恢复普通用户预算，保留所在分组。

用户与密钥原有预算继续叠加；套餐并发取用户上限与套餐上限中的较小有效值。Token/USD 套餐保留原单在途软预算语义，余额预留则支持并发；搜索缺少计量回执，不能绕过余额或 Token/USD 限制。

兑换码支持额度、并发增量、套餐。并发增量同时提高用户与当前订阅中的有限并发上限（不改模板）。代码只在生成时返回完整值，状态仅存 SHA-256 摘要和后缀；过期、停用、已使用时不可核销。核销与发放在同一个严格快照事务提交，失败全部回滚，已用记录不删除。最多 10000 个码，单批 100 个。

## 邮件与第三方登录

独立于 CodeArts/WorkBuddy/Trae 凭据，配置 SMTP 与 GitHub/LinuxDO 应用；配置读回清空秘密并报告是否已设置，空值保留、显式清除。邮件强制 TLS/STARTTLS，不提供明文 SMTP 降级。验证证明仅内存保存摘要，10 分钟有效、最多 5 次错误、单次消费，重启失效；找回密码对未知邮箱返回相同公开形状。密码哈希在存储锁外，最终以用户 revision 和已验证邮箱再次 CAS；重设密码撤销所有用户登录会话，保留 API 密钥。

OAuth 使用固定可信端点、PKCE S256、单次 state、浏览器绑定的 Lax HttpOnly Cookie；普通用户会话保持 Strict。令牌只用于即时 userinfo，不落盘、不发前端、不放 URL。稳定 provider/subject 唯一绑定，绝不按邮箱自动关联或注册。首次需密码登录后显式绑定；解除绑定撤销用户会话。没有真实 SMTP/OAuth 配置时展示未启用，不宣称真实发送和授权已经验收。
