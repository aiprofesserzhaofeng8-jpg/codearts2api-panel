# 模块、数据归属与依赖

状态：M3/M4/M5 已完成；M6–M7 能力按迁移计划实施。入口见 [总览](README.md)。路径均以 `backend/internal/` 为基准。

已实施部分：`accounts` 持有中立账号、账号管理协调、凭据代际错误及 WorkBuddy/Trae 凭据解析/刷新、登录/导入协调，以及 CodeArts 浏览器登录预览/确认、凭据互斥/缓存/提交协调；`app.Build` 装配平台和刷新回调；`upstream/codearts` 包装既有对话客户端及原生浏览器登录管理器，`upstream.BindStream` 绑定既有流式适配器；`gateway.Completions` 执行三平台已选中的普通单次对话，`gateway.RunPromptPolicy` 负责有限提示词恢复，`requestpolicy` 持有纯摘要识别/准备，`gateway.RunSummary` 持有完整性缓冲与有限截断重试，`PrepareSummaryAttempt` 持有备用型号准备协调，`RunAccountRetry`/`RunRecovery` 持有重试与租约生命周期；`protocol` 持有唯一的 SSE 聚合及流式选项实现。`access.Management` 持有分组与密钥管理协调，`config.Management` 持有全局设置与模型策略管理协调，`catalog.Runtime` 持有目录刷新/缓存及模型测活协调，`accounts.Credits` 持有积分查询和短缓存，`jobs.Rewards` 持有每日领奖状态/锁与单轮编排；Store 仍执行原持久化及准入事务。`catalog.Routing` 负责请求解析和分组映射，`gateway.RunSearch` 负责搜索编排，`protocol` 负责纯搜索查询及映射外壳；成长、活动、价格、管理员会话和应用生命周期也已按下方逐批记录迁出。M5 当前实现见下节；其余未来接口不能视为全部已实现。

## M5 当前归属

`gateway.Admission` 通过自身的 AdmissionStore 借用唯一 storage；`scheduler.Select/Legacy` 为纯算法。storage 保留权限复核/选号/占位事务、全部锁、黏性、冷却、预算预留和账本。usage 定义用量 DTO、成本及报表纯规则，access 定义预算检查与密码规则。store 只有别名/委托，架构检查禁止生产代码依赖它。没有第二份权限快照或并发计数。

旧原生槽现在是无平台方法约束的 NativeCredential 值，仍由 storage 兼容编码器一次解码并保留不可变指针；WorkBuddy/Trae 字节路径独立。下方 M3 逐批说明保留当时状态，当前事务保证和限制以 [ADR 0009](../adr/0009-runtime-storage-ownership.md) 为准。

统一登录后，`access.Users` 取代独立的 AdminSessions，持有服务端角色与共用的有界会话表；HTTP 在 `/auth` 处理登录，管理业务和消费者业务各自验证角色。`ConsoleApplication` 统一控制前端登录、API 生命周期及按角色挂载页面。该变更不迁移 schema、不改推理准入，见 [ADR 0014](../adr/0014-unified-role-authentication.md)。

## 责任表

表中入口为职责示意；HTTP 当前包仍叫 server，共享值分别归 accounts/access/config/usage，不为目标名称创建空 domain/httpapi 包。

| 模块 | 数据或行为归属 | 对外入口示意 | 不应承担 |
| --- | --- | --- | --- |
| `app` | 进程生命周期、依赖装配、配置热生效装配 | `Build`、`Start`、`Close` | 业务判定和平台协议 |
| `domain` | AccountID、GroupID、ModelRef、稳定错误分类等共享值 | 数据类型及无副作用校验 | 大而全的 Account 方法、数据库、平台常量 |
| `httpapi` | HTTP 参数、身份入口、协议错误输出 | 现有 `/admin/*`、`/v1/*` 路由 | 直接修改存储数据、自己选择账号 |
| `gateway` | Request/Attempt 生命周期、准入协调、重试预算、取消 | `Admission`、`RunAccountRetry`、`RunSummary` | 平台登录协议、持久化文件格式 |
| `accounts` | 账号元信息、凭据代际、登录会话、刷新协调、积分快照/查询短缓存 | `PreviewLogin`、`ConfirmLogin`、`CredentialsForAttempt` | 模型别名、各平台 HTTP 签名算法 |
| `access` | 管理会话、API Key、Group、白名单和分组映射配置 | `Authenticate`、`SaveGroup`、`CompileAccess` | 选择实际账号、解释上游模型能力 |
| `config` | 全局/平台/账号池/模型策略配置值、纯校验及管理保存协调 | `SettingsUpdate`、`UpdateSettings`、`SaveModelPolicy` | HTTP、原生凭据、准入事务、配置缓存和平台网络调用 |
| `catalog` | 目录刷新、模型测活、请求/搜索目标解析与分组映射；预设留 M6 | `Runtime.Refresh`、`Routing.Resolve`、`VisibleModels`、`Check`、`ResolveSearchTarget` | 保存分组、占用并发、自动猜测模型等价性 |
| `scheduler` | 候选排序和选择算法 | `Select(candidates, runtimeView)` | 独立租约计数器、文件 I/O、网络探测 |
| `policy` | 提示词配置语义、继承、清洗及请求准备 | `ResolvePolicy`、`PrepareBody` | 自行重试、改写用户/工具业务内容 |
| `usage` | 用量语义、价格版本、预算规则、结算记录 | `CheckBudget`、`Settle`、`Query` | 上游选号、普通调试日志替代账本 |
| `upstream` | 最小平台接口、注册表、标准错误与可选能力 | `Stream`、`Refresh`、`Models`、可选 `Search` 等 | HTTP 管理入口、分组权限、全局重试 |
| `storage` | 状态读写、迁移、原子提交、追加账本与回放 | 各业务定义的存储接口实现 | 持有另一套业务规则或选号算法 |
| `jobs` | 每日领奖与原定时检查、成长编排/锁/执行槽及未知对话检查点 | `RunDailyLoop`、`Rewards.Claim/Status/Batch/RunDaily`、`Growth.List/Accept/Claim/BeginRun` | 绕过原准入/账本执行计费对话 |
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

`accounts.ProviderCredentials` 通过使用方定义的 `ProviderCredentialStore` 读取最新快照并条件提交，只依赖 `CredentialAdapter.Normalize/Refresh` 解释不透明凭据。app 注入现有 Store 和动态续期开关，server 只查找原注册表并委托。按账号的刷新锁覆盖最新读取、规范化、刷新及提交；不持 Store 准入锁跨网络。`OperationLocks` 保留独立的刷新、领奖、成长任务锁实例。CodeArts 凭据协调见下述 CredentialRuntime，原 Manager 保留不可变 Session 指针与原生策略；不统一凭据格式或重写平台协议。

`accounts.ProviderLogins` 经 `ProviderLoginStore` 调用现有查询和原子保存，按操作传入 `LoginAdapters`，不保存另一份适配器注册表。授权流程状态与凭据留在业务内存中；返回类型仅含预览/保存结果，不含凭据或回调正文。HTTP 自行映射开始、等待、预览、保存、取消五种结果，以及原有错误状态码；UID 在 HTTP 端脱敏。平台协议、10 分钟会话/128 上限、30s 截止时间、原会话锁和保存语义均保持不变。登录服务不参与普通生成；账号管理其余入口尚未迁移。

`accounts.BrowserLogins` 经 `BrowserLoginBackend` 协调 CodeArts 原有浏览器授权，`upstream/codearts.BrowserLogin` 包装同一原生 LoginManager。服务负责预览、确认保存、显式替换和删除后重放保护，适配器解释 STS JSON、核验身份并调用原回调入口。原生单会话 Commit 锁仍包住保存与缓存失效；不新建会话注册表或复制凭据，Store 的查重与保存仍为原事务。CodeArts 普通凭据协调见下述运行时，平台专有的读取与刷新继续归 Manager。

`accounts.CredentialRuntime[T]` 只协调旧 Session 槽的不可变代际；CodeArts Manager 内嵌同一实例，原 Latest/Commit/Persist/Logf 回调继续由 app 装配。按账号 mutex 覆盖最新快照、平台解析与提交，短缓存和失效共用原锁域；与 WorkBuddy/Trae 的 OperationLocks 分离。平台保留 IDE I/O、DPoP、默认解析参数、错误和内容比较，Store 保留唯一 CAS；不以反射或 JSON 判断代际，不新增缓存或统一凭据格式。原 Manager 的 resolve 是平台策略实现，Session 是单向兼容入口。

`accounts.Management` 负责账号管理的查询、提交与失效顺序，只依赖窄 Store 接口及原生导出/重读能力。删除、并发走原严格保存事务；启停、暂停、导入及重读成功后的收尾保存沿用记录保存失败的旧行为，原生轮转 CAS 错误仍向上传递。HTTP 保留 wire DTO、状态码、凭据导出提示和成功删除后的积分视图缓存清理；CodeArts 手工、JSON、桌面构造与原生序列化归 `upstream/codearts`。WorkBuddy/Trae 导出直接保留不透明凭据字节，不调用原生解析器；账号服务没有新注册表、准入锁或凭据缓存。

`gateway.PrepareSummaryAttempt` 经 `SummaryTargetSource` 读取当前强制型号、目录、冷却和纯摘要选项；数据源不解释原生凭据、不选号、不新增缓存。更换目标仍限定在相同平台/地区并复用已绑定传输；首次准备沿用准入结果，重试先复核当前权限和已发生用量的预算进度。server 投影 Context 并委托既有 `checkRequestBudget`，原分组映射与原始白名单校验保留原入口；Store 的原子准入和账本未迁出。`requestpolicy.PrepareSummaryModel` 仅处理已授权型号的协议外壳；换型号删除原顶层 reasoning 控制，再应用备用型号自己的摘要策略。

`access.Management` 只管理分组与密钥配置协调，经 `ManagementStore` 调用原 Store 事务，并借用当前目录/别名解析函数。原始型号来源必须精确命中目录；白名单沿用别名归一化和未知型号保留行为。更新分组时缺省/null 映射继承旧值，显式空列表清除。密钥引用保留原值、稳定 hash 和旧脱敏值的匹配顺序；管理员认证及明文找回入口仍在 HTTP 层。`APIKey`、`AccountGroup`、`ModelMapping`、`KeyBudget`、`BudgetUsage` 的规范定义归 access，Store 以类型别名兼容 schema 6。原子权限发布、预算预留/重置、准入和账本仍属 Store；管理服务不进入推理路径，不建立权限副本。全局设置/模型策略及认证会话尚未随本批迁移。

`config.Management` 经 `ManagementStore` 借用原设置读取/发布、策略严格保存和状态落盘接口。设置先完成全部校验，再调用原 UpdateSettings，Save 失败继续记日志；HTTP 随后应用原身份头与限流。模型策略借用实时目录/别名，仅接收 ID、平台、地区；保留校验顺序、相同平台/地区备用要求、强制型号约束和 CodeArts 独有摘要保护限制。Settings、ProviderSettings、PoolSettings、ModelPolicy 的规范定义归 config，Store 类型别名保持旧 JSON；原纯校验/默认策略迁移不改变算法。Store.DefaultSettings 中原生常量投影仍在兼容入口，不将 CodeArts 导入 config。价格值/校验与管理协调见 M3.19；计量/预算/账本拆分留 M5，目录推荐生成/公共合并已在 M3.21 迁入 config。

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
| `store/pool.go` | `gateway` 协调 + `scheduler` 算法 + `storage` 事务 | 已完成；状态与锁保留一个所有者 |
| `store/groups.go`、密钥管理 | `access` + `storage` | 配置协调和中立模型已归 access；Store 类型别名兼容，原分组事务、权限发布和预算重置由 storage 唯一持有 |
| `registry`、`server/group_model_mapping.go` | `catalog` + `gateway` | 目录、解析、尝试期型号替换分别归位 |
| `requestpolicy`、`server/request_policy.go` | `policy` + `gateway` | 提示词恢复已归 gateway，纯提示词/摘要准备归 requestpolicy；公共继承留后续阶段 |
| `codearts.Account`、凭据提交 | `accounts` + `storage` 兼容编码器 | 元信息及三平台凭据协调已迁出；CodeArts 原生策略、旧状态编码及 Store 条件提交保留，互斥与缓存归 accounts 运行时 |
| `provider`、`codearts`、`workbuddy`、`trae` | `upstream` 父包及各平台子包 | 三平台对话边界已接入；原生实现和其余能力待迁，禁止重写成熟协议实现 |
| `store/usage*`、`pricing`、`budget` | `usage` + `storage` | 结算规则与账本 I/O 分开，预留由准入协调 |
| `server/growth*`、`claim.go` | `jobs`、平台能力、管理 handler | 任务执行、上游行为与 HTTP 分开 |
| `server/web/*` | `frontend` | 先接构建再逐页迁移 |
| `server/web.go` | `webui` | 嵌入目录受构建控制，不嵌入任意运行目录 |

## 扩展验收

新增一个使用现有协议的平台，应能通过“实现适配器、注册、能力配置、可选登录表单、测试”完成普通接入。确实需要新的通用协议语义时，单独定义扩展点并验证旧平台。通过新增空实现、吞掉不支持错误或把所有分支搬到同一个新文件，不算完成模块隔离。

以下为各批迁移时的范围记录，当前完整边界见末节。

M3.15 的 catalog Runtime 借用唯一 Registry，只持有原刷新状态及原三把独立锁。HTTP 投影平台操作和宽松解码，CodeArts 获取/原生 Probe 由 upstream 包装，其余测活继续走原 chatOnce；不创建平行目录、解析器或普通推理入口。目录解析/映射仍由 Registry、access、Store 的既有边界负责，不因目标表存在 `Resolve` 就重复实现。

M3.16：积分数量快照归 accounts，不属于 API 美元费用/预算账本。CodeArts 仅保留 CreditInfo/CreditCategory 别名，原生解析不改；WorkBuddy/Trae 继续返回原 JSON 并按原顺序解析。jobs 每日领奖调用原适配器，不自行解析凭据，不写准入或推理健康状态。领奖、成长、凭据的三组账号锁仍独立；自动领取定时器、活动列表与邀请 HTTP 中剩余的原生调用待后续迁移。

M3.17：成长编排与检查点归 jobs.Growth，app 装配固定的原 `.growth.json` 路径。HTTP 保留账号重读与凭据入口，付费回调只改变检查点所有者的单向委托；Store 准入和账本仍不拆分。原子检查点实现不是第二个账号 Store，也不能用于普通请求或重复提交未知动作。

M3.18：jobs.Welfare 借用原 Store 选择账号和生成账号选项，不保存调度状态；upstream/codearts 包装活动列表、邀请调用和原生结果投影。HTTP 保留 DTO/方法/默认值/错误码，外部每日状态仍归 Rewards，列表与邀请不扩展彼此的回退规则和平台能力。无新锁、缓存、上游重试或普通推理调用。


M3.19：usage.ModelPrice 为规范价格类型，Store 仅保留兼容别名；usage.Management 经原严格 SetPriceAndSave 管理价格，未复制计量、历史快照或保存锁。access.AdminSessions 持原会话摘要表及互斥，HTTP 保留 Cookie、同源、失败限流和审计，密码哈希/恢复事务仍沿用 Store。两者由 app 显式装配，既无新推理调用，也无新清理协程/缓存。

M3.20：app.Start/Runtime.Close 持 HTTP 运行实例、后台启动及原停机顺序，jobs.RunDailyLoop 持原领奖定时算法；main 保留命令行/环境、桌面导入/导出、首次访问文件、OS 信号和致命退出。后台共享取消与独立刷新期限保持原样，不 join 已运行任务，不强制关闭有效 HTTP 响应。server 的 StartClaimLoop 只作单向兼容。app.Build 无副作用、所有平台/Store/普通推理和页面不变，边界收尾与 M3 整体验收仍待完成。

M3.21：accounts.Probes 持账号探活串行循环、30/60s 截止时间、目录成功发布与结果保存顺序。原生会话/脱敏提示由 upstream/codearts 投影，HTTP 保留 wire DTO 与原宽松解码。config.ModelPolicies 借用 Store/Registry 提供实时推荐，CommonModelPolicy 只合并已获授权来源的建议；无新缓存、锁或推理调用。执行 `make architecture`（可用 GO/PYTHON 指定工具），检查生产 `go list -json` 的直接导入并测试检查器；它不把旧 Registry/apierr/Store 桥接当作迁移完成。搜索执行与统一请求型号解析仍待迁。


## M3 完成后的调用边界

`server` 将 HTTP 身份和 Context 投影给 `catalog.Routing`，从同一目录获得已授权原型或请求私有的 `ModelPlan`；计划不缓存权限、不选号。Store 的原子入口仍将实时权限、候选约束和并发占位一起处理。Messages 的 Trae thinking 排除只收窄本请求的候选。HTTP 在已选中账号后调用计划校验及 `protocol` 外壳替换，不修改工具数据或普通转发路径。

`gateway.RunSearch` 只执行已有能力；原生搜索使用当前凭据与同一重试/冷却入口，SearxNG 只借用预算和原子租约。两条路径均沿用原 20s 上限及更小的请求期限，不增加请求、锁或重试。`catalog.ResolveSearchTarget` 保留平台/地区边界；`protocol.SearchQuery` 保留原文本提取规则。

`make architecture` 检查生产导入的直接边和内部传递路径，防止中立业务借旧错误映射/目录工具间接引入 HTTP、Store 或具体平台。`apierr` 保留 HTTP/原生错误映射，`NoAccount` 和 `ModelNotFound` 单向别名到业务类型；目录不再导入 `apierr`。Registry、Store 和原生协议包作为既有实现继续保留，检查器不会把这些遗留桥接整体宣称为新结构。详情及验证限制见 [M3 完成验收](../acceptance/STRUCTURE-M3-COMPLETE-2026-11-02.md)。
