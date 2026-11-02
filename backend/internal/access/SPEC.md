# 访问配置与管理员会话

access 定义 APIKey、AccountGroup、ModelMapping、KeyBudget 和 BudgetUsage；Management 只协调配置，原子权限发布、预算预留和落盘仍由原 Store 负责。

M3.19 的 AdminSessions 持有原进程内会话注册表及原互斥锁。零值可用，app 显式装配唯一实例；只保存 token 和当前存储密码表示的 SHA256 摘要，8 小时过期、签发时清理过期项、达到 128 项时淘汰最早过期项。相同过期时间的原 map 遍历顺序不作新承诺，不建立新索引。

Valid 保留原短路和锁顺序：先持会话锁，找到未过期 token 后才读取当前密码快照，按原双哈希恒定时间比较；未知或过期 token 不读取密码。Issue 复用 HTTP 已生成的随机 token 和时刻，Revoke 只撤销摘要。无后台清理、持久化、额外缓存或第二份密码版本；重启、退出或密码存储表示变更后仍按原规则失效。

Cookie 属性、HTTP 同源/跨站检查、认证头兼容、20 次失败限流和审计留在 server。密码哈希/初始密码恢复及事务仍在原 Store；普通推理 API Key 鉴权、准入与账号凭据锁不进入本会话服务。

M6：AccountGroup 增加请求默认、客户端偏好及 ModelRule 的独立 visible/callable。可空字段区分继承与 false；快照深拷贝，来源禁用与原白名单共同限制映射。Management 只接受当前目录、新公开名或保留的旧缺失来源，预设不自动扩张权限；最终规则与成员仍由 storage 原子准入复核。
