# 型号身份、能力传输与会话稳定性调查

日期：2026-11-07。范围：源码与已有实测证据只读调查。没有修改生产逻辑、账号配置、模型定义或客户端配置，没有新增真实生成请求。本机包含未部署的 R1/R2 改动；本报告不把源码机制等同于运行中的配置已触发。

## 结论与证据边界

同类风险不只存在于 WorkBuddy。当前至少有遗留跨型号别名、平台内部槽位身份不透明、图片传输限制、提示词替换、会话切源和响应型号归一化等不同原因，需要分别处理。

已有证据只确认 WorkBuddy 图片请求的配额型号变化；尚未证明其他平台在服务器内部换了底模，亦没有受控缓存命中率对比。识图或工具调用成功不能证明底模身份，顶层 model 字段也不能单独作证。

## 调查发现

| 编号 | 发现 | 证据性质 | 影响与边界 |
| --- | --- | --- | --- |
| A1 | WorkBuddy 请求 glm-5.3 的图片任务，原生 response.model 仍为 glm-5.3，但 usage.quota.model 为 glm-5.3-flash；纯文本配额型号为 glm-5.3 | 2026-11-06 原生实测：7 次原生生成，3 次 glm-5.3 图片用例均出现该配额分类 | 强烈提示 Flash 相关路由或处理阶段；不能断言所有计算都换为 Flash。未发现本地 WorkBuddy 适配器主动改成 Flash |
| A2 | 遗留兼容别名把 gpt-4o、gpt-4o-mini、claude-3-haiku 等指向 glm-5.3；gpt-5、claude-3-5-sonnet 等指向 GLM-5.2 | 静态代码确认；实际使用取决于直接型号是否存在、统一定义、别名及访问规则 | 会在可达的旧兼容路径造成“客户端名称与实际请求型号不同”；不是上游偷偷换模。不能一律宣称当前请求均命中该路径 |
| A3 | Qoder 的 gmodel/kmodel_latest 等是平台槽位。默认名称生成会核对显示名，但已有统一模型在构建候选时未见同等身份核对 | 静态代码风险确认，未实测模拟目录漂移 | 槽位保持原 ID、目录显示名变为另一个型号时，旧绑定可能继续以旧公开名称调用。R2 中 qfmodel/qmodel_38max 的能力证据名称检查不等于所有已有映射获得身份检查 |
| A4 | Trae 本地适配器只接受 text 内容块，图片/音频会在发送上游前被拒绝；Kiro 允许用户图片，但系统、助手历史和工具结果图片有位置限制 | 静态代码确认 | 是当前传输链路限制，不能据此认定 Trae/Kiro 上游或其底模不支持。需要独立核对原生附件协议 |
| A5 | custom 提示词模式删除客户端 system/developer 消息并加入配置提示词；passthrough 的特定错误恢复也可改为中性提示词；sanitize 可改写少量固定客户端模板 | 静态代码确认，当前运行配置是否触发未读取 | 会影响行为或缓存前缀，不能把所有表现变化归因于模型。默认值包含 fallback=true、sanitize=true，但已有部署可能有独立配置，不推断本机当前值 |
| A6 | 已有会话粘性，但不是来源锁定：账号满载、冷却、被排除、来源能力不满足或绑定过期后会重新选号；失败重试也可能更换账号/来源 | 静态代码确认 | 能力过滤发生后，原来不满足图片要求的文本来源不能继续入选，可能切到另一平台。是可用性与稳定性的现有取舍，不能保证缓存仍复用 |
| A7 | 无显式会话 ID 时，用 API Key、model、system 内容和首条 user 内容构造会话哈希 | 静态代码确认 | 首条历史被压缩/截断、系统提示变化、密钥或名称变化，会得到不同绑定；相同初始内容的独立会话也可能共享绑定。当前未核对实际 Pi/Codex 请求头，不能断言客户端总缺少会话 ID |
| A8 | 多个适配器及共享响应会把顶层 model 统一为请求名或公开名；TeleAgent chat-pro/chat-flash 等和 Qoder/Kiro auto 等属于平台服务入口，不能仅凭名字认证底模 | 静态代码确认；实际隐藏路由未获证明 | 归一化有协议兼容用途，但会削弱客户端辨别上游原始型号的能力。现有日志记录所选物理来源，不等于记录了平台内部实际执行型号 |

## 源码位置

- 遗留兼容别名及解析：[registry.go](../../backend/internal/registry/registry.go)，aliases、Resolve。
- WorkBuddy 请求保持 input.Model、响应型号归一化：[client.go](../../backend/internal/workbuddy/client.go)，Stream。
- Qoder 名称核对：[default_models.go](../../backend/internal/catalog/default_models.go)，defaultModelName；既有候选构建：[unified_routing.go](../../backend/internal/catalog/unified_routing.go)，buildUnifiedPlan。
- Trae 图片拒绝：[chat.go](../../backend/internal/trae/chat.go)；Kiro 图片位置限制：[chat.go](../../backend/internal/kiro/chat.go)。
- 提示词策略：[request_policy.go](../../backend/internal/requestpolicy/request_policy.go)、[prompt_policy.go](../../backend/internal/gateway/prompt_policy.go)、[sanitize.go](../../backend/internal/requestpolicy/sanitize.go)。
- 粘性选择及满载/冷却检查：[pool.go](../../backend/internal/storage/pool.go)；会话哈希：[routing.go](../../backend/internal/server/routing.go)；换号重试：[account_retry.go](../../backend/internal/server/account_retry.go)。
- 响应公开名称：[model_envelope.go](../../backend/internal/protocol/model_envelope.go)。

WorkBuddy 原始证据留在本机忽略目录 artifacts/workbuddy-glm53-native/：REPORT.md、native-results.jsonl、summary.json；不复制认证信息或原始运行状态到公共文档。

## 与缓存、性能波动的关系

本轮可以确认存在影响模型选择或输入内容的代码路径，但不能证明本机缓存命中率已因某一项下降。切换来源、账号或实际模型，可能进入不同缓存域；改变 system、工具或历史前缀也可能影响缓存。这些需要按实际平台缓存规则及同条件对照测试确认。客户端展示名称不变不代表来源或缓存域不变。

摘要备用型号是另一项显式配置能力，只针对摘要任务，不应等同于普通对话自动换模，亦不能单独据此指控对话路由错乱。

## 建议处理顺序

1. 优先梳理遗留跨原生型号别名，以及 Qoder 等动态槽位的已保存绑定；先列出受影响配置再迁移，避免直接删除导致旧客户端不可用。
2. 核对真实运行中的提示词设置和客户端会话 ID，只记录必要元数据。复用原粘性机制，不另建调度器。
3. 调查 Trae 原生图片传输契约，区分平台不支持、账号无权限和本地未实现。不得以更改支持声明替代协议实现。
4. 之后进行少量固定账号、固定来源、固定提示词、固定历史的 A/B 验证，再与允许切源的情况比较。优先读取真实 cached_tokens/usage 字段；字段缺失记为未知，不能按零命中计算。

本报告属于调查，不是修复完成证明；没有新增后台探测、存储或每请求网络调用。

## 后续处理

同日已完成第一批源码修复及回归，见 [型号身份与输入稳定性修复](MODEL-IDENTITY-STABILITY-FIX-2026-11-07.md)。本调查保留修复前发现；当前状态及未完成范围以该修复记录为准。
