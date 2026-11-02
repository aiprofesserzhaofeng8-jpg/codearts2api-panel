# 访问配置与统一角色会话

access 定义 APIKey、AccountGroup、ModelMapping、KeyBudget 和 BudgetUsage；Management 只协调配置，原子权限发布、预算预留和落盘仍由原 Store 负责。

Users 持有管理员和普通用户共用的进程内会话注册表，app 显式装配唯一实例。令牌仅保存摘要、8 小时过期，每身份最多 16 项、全站最多 2048 项，签发时清理及淘汰最早过期项。旧 AdminSessions 已删除，不能另建管理认证注册表。

Login 只接受用户名和密码；保留名 admin 对应固定服务器身份 builtin:admin，复用原管理员密码及用户登录的两个密码计算槽。普通用户 View 的角色固定为 user，OAuth OpenSession 必须从持久化消费者查身份。角色不能从请求或前端读取；Authenticate 在释放会话锁后复核管理密码摘要，或普通用户的 enabled/revision。未知及过期令牌直接拒绝。无后台清理、持久化或权限缓存；退出、重启、对应密码或 revision 变化后失效。

Cookie 属性、HTTP 同源/跨站检查、失败限流、角色中间件和审计留在 server。管理密码请求头、Bearer 密码及本机自动解锁均不再支持。密码哈希/初始密码恢复及事务仍在原 Store；普通推理 API Key 鉴权、准入与账号凭据锁不进入本会话服务。见 [统一认证决定](../../../docs/adr/0014-unified-role-authentication.md)。

M6：AccountGroup 增加请求默认、客户端偏好及 ModelRule 的独立 visible/callable。可空字段区分继承与 false；快照深拷贝，来源禁用与原白名单共同限制映射。Management 只接受当前目录、新公开名或保留的旧缺失来源，预设不自动扩张权限；最终规则与成员仍由 storage 原子准入复核。
