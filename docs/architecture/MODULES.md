# 模块、数据归属与依赖

状态：目标设计。入口见 [总览](README.md)。路径均以未来 `backend/internal/` 为基准。

已实施部分：`accounts` 持有中立账号、凭据代际错误及 WorkBuddy/Trae 凭据解析/刷新、登录/导入协调，以及 CodeArts 浏览器登录预览/确认协调；`app.Build` 装配平台和刷新回调；`upstream/codearts` 包装既有对话客户端及原生浏览器登录管理器，`upstream.BindStream` 绑定既有流式适配器；`gateway.Completions` 执行三平台已选中的普通单次对话，`gateway.RunPromptPolicy` 负责有限提示词恢复，`requestpolicy` 持有纯摘要识别/准备，`gateway.RunSummary` 持有完整性缓冲与有限截断重试，`RunAccountRetry`/`RunRecovery` 持有重试与租约生命周期；`protocol` 持有唯一的 SSE 聚合及流式选项实现。`server`、`store`、平台包的其余职责仍按下表分批迁移，表中目标接口不能视为全部已实现。

## 责任表

| 模块 | 数据或行为归属 | 对外入口示意 | 不应承担 |
| --- | --- | --- | --- |
| `app` | 进程生命周期、依赖装配、配置提交协调 | `Build`、`Start`、`Close` | 业务判定和平台协议 |
| `domain` | AccountID、GroupID、ModelRef、稳定错误分类等共享值 | 数据类型及无副作用校验 | 大而全的 Account 方法、数据库、平台常量 |
| `httpapi` | HTTP 参数、身份入口、协议错误输出 | 现有 `/admin/*`、`/v1/*` 路由 | 直接修改存储数据、自己选择账号 |
| `gateway` | Request/Attempt 生命周期、准入状态、重试预算、取消 | `Execute`、`BeginRequest`、`AcquireAttempt` | 平台登录协议、持久化文件格式 |
| `accounts` | 账号元信息、凭据代际、登录会话、刷新协调 | `PreviewLogin`、`ConfirmLogin`、`CredentialsForAttempt` | 模型别名、各平台 HTTP 签名算法 |
| `access` | 管理会话、API Key、Group、白名单和分组映射配置 | `Authenticate`、`SaveGroup`、`CompileAccess` | 选择实际账号、解释上游模型能力 |
| `catalog` | 原始模型目录、映射解析、已验证预设、能力元数据 | `Resolve`、`CompileBindings`、`ListVisible` | 保存分组、占用并发、自动猜测模型等价性 |
| `scheduler` | 候选排序和选择算法 | `Select(candidates, runtimeView)` | 独立租约计数器、文件 I/O、网络探测 |
| `policy` | 提示词配置语义、继承、清洗及请求准备 | `ResolvePolicy`、`PrepareBody` | 自行重试、改写用户/工具业务内容 |
| `usage` | 用量语义、价格版本、预算规则、结算记录 | `CheckBudget`、`Settle`、`Query` | 上游选号、普通调试日志替代账本 |
| `upstream` | 最小平台接口、注册表、标准错误与可选能力 | `Stream`、`Refresh`、`Models`、可选 `Search` 等 | HTTP 管理入口、分组权限、全局重试 |
| `storage` | 状态读写、迁移、原子提交、追加账本与回放 | 各业务定义的存储接口实现 | 持有另一套业务规则或选号算法 |
| `jobs` | 后台任务状态机、限并发、恢复和调度 | `Start`、`Run`、`Reconcile` | 绕过网关执行计费对话 |
| `webui` | 嵌入产物、静态路由、CSP、SPA 路由回退 | `Handler` | 管理业务或开发服务器自动公开 |

这里的函数名是设计约定，不是当前已存在的 Go API。

## 类型和数据只定义一次

账号元信息归 `accounts`，平台凭据归对应适配器解释，持久化表示归 `storage`。共享模块只传账号 ID、平台/地区和必要快照，不传一个包含所有平台字段的可变 Account 指针。

Group 及其映射配置由 `access` 保存；`catalog` 接收平台无关的绑定值并解析，不反向读取 `access.Service`。这样既有唯一配置来源，也不会形成 `access → catalog → access`。

价格和历史结算由 `usage` 管理；请求准入中正在使用的预算预留属于 `gateway` 运行状态。预算计算函数由 `usage` 提供，预算修改、结算发布和预留的并发协调由准入入口负责，不建立两个各自允许放行的预算计数器。

共享的 `ModelRef` 明确区分 `provider`、`scope`、`upstream_id` 和原始目录 ID；公开名称是请求/分组属性。保留现有外部字符串格式，内部不再靠字符串前缀承载全部业务状态。

## 依赖约束

1. `domain` 仅依赖标准库。`protocol` 保持纯转换，不能依赖 HTTP、存储或账号池。
2. `upstream` 父包只定义平台契约、通用错误、注册表；子包可以依赖父包，父包不能导入具体平台。具体平台由 `app` 注册，平台之间禁止互相导入。
3. `gateway` 调用账号、访问、目录、调度、策略、用量与平台接口；这些模块不能反向依赖 `gateway`。后台任务需要生成模型内容时，由 `jobs` 调用网关入口。
4. 跨模块接口放在使用方，返回业务结果或只读快照。只有跨多个模块确实共享的值才进入 `domain`；接口不做一对一机械包裹。
5. `storage` 实现使用方定义的窄接口，可以引用业务数据类型；业务模块不导入具体文件存储实现。需要跨记录提交时显式提供事务接口，不把它分解为若干独立 `Save`。
6. `httpapi` 调用服务方法；`app` 是具体实现装配点。不能通过全局 service locator 或可变包级单例规避依赖规则。
7. 配置提交经使用方定义的发布接口交给 `app` 协调，不让 `access`、`accounts` 导入网关来“手动同步缓存”。

实施时用 `go list -json` 检查导入边界，并配合 frontend lint 检查 feature 之间的越界引用。架构约束检查针对禁止依赖，不硬编码文件数量或行数。

## 平台能力

沿用最小 Adapter 与可选能力的思路，搜索、成长任务、设备登录不强塞进所有平台的必选接口。

平台报告的能力与模型报告的能力分开：例如平台能登录，并不代表每个模型都支持工具或 thinking 参数。运行时只在已知兼容来源中选择；未知能力保留现有明确限制，不通过一次目录重构扩大支持声明。

CodeArts 以包装现有客户端和 Manager 的方式接入统一边界，先保留签名、DPoP、SQLite 和 Windows DPAPI 实现。通用账号不再导入 CodeArts Session；旧 `session` 字段由存储兼容编码器读取和写回，转换只发生在读写边界。其内部包何时细拆，由对应协议测试覆盖决定。

当前对话边界为使用方 `gateway.ChatSession`：`Chat`、`Stream` 和平台判断的空响应回退条件。`upstream/codearts.Session` 绑定现有不可变凭据指针，并在回调前过滤 CodeArts 业务错误；不选账号、不刷新、不结算。`provider.Adapter` 及 WorkBuddy/Trae 暂保留原接口，不将 CodeArts 凭据序列化后塞进它。普通单次执行已经脱离 HTTP，提示词、摘要及全局恢复由 gateway 协调，server 保留平台策略及授权。旧 `codearts.AggregateSSE` 只单向委托 `protocol.AggregateSSE`，供既有平台内测试兼容，迁完调用方后删除。

最小 `gateway.StreamTransport` 仅包含 `Stream`；CodeArts 的 `ChatSession` 扩展该能力。WorkBuddy/Trae 用 `upstream.BindStream` 借用已经 Normalize 后的 JSON 凭据及不可变请求标识，不增加凭据解析或序列化；其原适配器只需原有的 `Stream` 方法。`CompleteStream` 保留原先非流式聚合的 32MiB 上限，普通流式仍即时交付。server 准备该次凭据、投影平台提示词配置和摘要豁免，再由 gateway 恢复/执行；server 的恢复 hook 只提供原尝试计量及权限/预算复核，WorkBuddy/Trae 刷新互斥已归 accounts。平台目录、搜索和奖励仍使用原接口；登录/导入协调已迁入 accounts，原生协议继续使用平台接口。

`gateway.PromptPolicy` 是平台配置的推理投影，`requestpolicy.SummaryOptions` 只包含本次摘要的输出预算和思维控制；两者不定义新的持久化配置。提示词恢复先结束旧尝试并复核现有预算/权限，再准备替换请求并开始新尝试。摘要识别与准备不包含平台调用或重试；`gateway.RunSummary` 执行既有摘要流程，保留 8MiB 缓冲和仅 length 最多重试一次的边界。server 的 Prepare/Start/Usage/Result hook 分别提供备用型号与权限预算、尝试开始、分片用量、结束健康写入；不将 Store 或平台错误分类导入 gateway。

`gateway.RetryDriver` 接收使用方的原子准入及平台策略，不直接导入 Store 或平台。`RetryAttempt` 携带实际型号、带总期限的上下文及释放函数；同一租约内的短恢复必须回报是否消耗另一个尝试槽。server 的 `accountRetryDriver` 保留当前权限/预算和健康写入，`accountRecovery` 保留 WorkBuddy 进入窗口及授权。`CommittedStreamError` 归 gateway，HTTP 入口只报告可见输出边界，保留原始错误链。取消先于平台健康分类，正常结束及分类时租约已经释放；不能把这个抽取视为准入锁或调度算法已经拆出。

`accounts.ProviderCredentials` 通过使用方定义的 `ProviderCredentialStore` 读取最新快照并条件提交，只依赖 `CredentialAdapter.Normalize/Refresh` 解释不透明凭据。app 注入现有 Store 和动态续期开关，server 只查找原注册表并委托。按账号的刷新锁覆盖最新读取、规范化、刷新及提交；不持 Store 准入锁跨网络。`OperationLocks` 保留独立的刷新、领奖、成长任务锁实例。CodeArts 的 Manager、不可变 Session 指针及专用代际提交继续原样工作；本批不统一凭据格式或重写平台生命周期。

`accounts.ProviderLogins` 经 `ProviderLoginStore` 调用现有查询和原子保存，按操作传入 `LoginAdapters`，不保存另一份适配器注册表。授权流程状态与凭据留在业务内存中；返回类型仅含预览/保存结果，不含凭据或回调正文。HTTP 自行映射开始、等待、预览、保存、取消五种结果，以及原有错误状态码；UID 在 HTTP 端脱敏。平台协议、10 分钟会话/128 上限、30s 截止时间、原会话锁和保存语义均保持不变。登录服务不参与普通生成；账号管理其余入口尚未迁移。

`accounts.BrowserLogins` 经 `BrowserLoginBackend` 协调 CodeArts 原有浏览器授权，`upstream/codearts.BrowserLogin` 包装同一原生 LoginManager。服务负责预览、确认保存、显式替换和删除后重放保护，适配器解释 STS JSON、核验身份并调用原回调入口。原生单会话 Commit 锁仍包住保存与缓存失效；不新建会话注册表或复制凭据，Store 的查重与保存仍为原事务。CodeArts 普通 Manager 解析/刷新尚未迁出。

WorkBuddy 成长任务事件、依赖、官方接口留在 `upstream/workbuddy`；`jobs` 管排程、同账号串行、未知结果恢复。前端工作流只消费状态，不复制任务完成算法。

## 旧代码迁移表

| 迁移前位置 | 目标归属 | 迁移方式 |
| --- | --- | --- |
| `main.go` 及其测试 | `cmd/panel`、`app` | 参数与业务初始化分别移动，构建注入版本保持一致 |
| `server.New`、Provider 注册 | `app` | 已完成依赖装配；server.New 接收依赖，HTTP 运行状态仍归 server |
| `server.go` 中 HTTP 路由和鉴权入口 | `httpapi` | 先代理现有 handler，再逐步抽出业务 |
| `server/chat.go` 的重试和请求生命周期 | `gateway` | 三平台普通单次执行已迁出；提示词恢复已迁；摘要执行已迁；全局重试循环已迁；平台策略和原子准入暂留，保留取消和输出边界 |
| `server/anthropic.go`、`responses.go` | `httpapi/inference` + `protocol` | HTTP 与纯转换分开，避免复制转换器 |
| `protocol`、`responsescompat` | `protocol` 中清楚命名的实现 | 首批可保留旧包名；来源许可和测试随迁移保留 |
| `store/pool.go` | `gateway` 准入状态 + `scheduler` 算法 | 先保留旧 Store 原子入口，再迁移锁与状态 |
| `store/groups.go`、密钥管理 | `access` + `storage` | 分组事务、预算重置等保持原子语义 |
| `registry`、`server/group_model_mapping.go` | `catalog` + `gateway` | 目录、解析、尝试期型号替换分别归位 |
| `requestpolicy`、`server/request_policy.go` | `policy` + `gateway` | 提示词恢复已归 gateway，纯提示词/摘要准备归 requestpolicy；公共继承留后续阶段 |
| `codearts.Account`、凭据提交 | `accounts` + `storage` 兼容编码器 | 元信息及 WorkBuddy/Trae 解析/刷新协调已迁出；CodeArts Manager、旧状态编码及条件提交暂留原实现，保留代际校验 |
| `provider`、`codearts`、`workbuddy`、`trae` | `upstream` 父包及各平台子包 | 三平台对话边界已接入；原生实现和其余能力待迁，禁止重写成熟协议实现 |
| `store/usage*`、`pricing`、`budget` | `usage` + `storage` | 结算规则与账本 I/O 分开，预留由准入协调 |
| `server/growth*`、`claim.go` | `jobs`、平台能力、管理 handler | 任务执行、上游行为与 HTTP 分开 |
| `server/web/*` | `frontend` | 先接构建再逐页迁移 |
| `server/web.go` | `webui` | 嵌入目录受构建控制，不嵌入任意运行目录 |

## 扩展验收

新增一个使用现有协议的平台，应能通过“实现适配器、注册、能力配置、可选登录表单、测试”完成普通接入。确实需要新的通用协议语义时，单独定义扩展点并验证旧平台。通过新增空实现、吞掉不支持错误或把所有分支搬到同一个新文件，不算完成模块隔离。
