# 开发约定

## 当前布局与阶段

Go 模块位于 `backend/`，模块名为 `codearts2api`。服务入口为 `backend/cmd/panel`，诊断入口为 `backend/cmd/probe`。当前沿用成熟平台底层实现及嵌入页面，逐批抽取业务边界；独立前端和存储拆分按 `docs/architecture/MIGRATION.md` 后续实施。

M3 第一批已将中立账号迁入 `internal/accounts`，平台依赖装配迁入 `internal/app`。共享代码使用 `accounts.Account`；`codearts.Account` 仅为迁移别名。旧 `session` 槽由 store 兼容编码器读取、CodeArts 解释，不能为新平台继续往该接口堆方法或往中立账号添加平台字段。请求路径不能为了读取旧会话而反复解析 JSON。

M3 第二批将 CodeArts 对话传输包装为 `internal/upstream/codearts`，普通单次执行归 `internal/gateway.Completions`，SSE 聚合及请求流式选项归 `internal/protocol`。适配器借用已选中的不可变凭据；不能另建 JSON 凭据解码路径。gateway 不导入 HTTP、Store 或具体平台，不增加选号、刷新或重试层。该批尚未迁移的摘要、全局重试和其他平台执行按后续批次逐项处理。

M3 第三批将 WorkBuddy/Trae 的单次流式执行及非流式聚合也接入 `gateway.Completions`。`upstream.BindStream` 只借用已解析凭据并传递型号、会话和请求标识；原生协议、登录、搜索、奖励仍归各平台。两平台只实现最小流式能力，不伪造原生 Chat 或空响应回退接口。32MiB 聚合上限保持原值；凭据解析/刷新互斥、提示词恢复、全局重试和摘要编排仍待后续迁移。

M3 第四批将提示词准备/恢复编排迁入 `gateway.RunPromptPolicy`，摘要识别/准备归 `requestpolicy`。server 仅投影平台配置、判定摘要豁免，并通过两个 hook 保留原尝试计量及预算/分组权限复核。顺序必须是结束旧尝试、复核预算/权限、准备替换请求、开始新尝试；收到任何上游分片后不能恢复。摘要的完整性缓冲、备用型号选择、重试、冷却和结算仍在原实现，本批没有扩展其他平台摘要能力或公共提示词继承。

M3 第五批将摘要完整性验证、8MiB 私有缓冲及最多一次截断重试迁入 `gateway.RunSummary`，使用既有 `StreamTransport`，不增加平台能力。server 通过 Prepare/Start/Usage/Result hook 保留备用型号解析、预算与分组权限、尝试计量、用量和模型冷却；准备成功后才开始计量。错误仍为请求级终止，不能经外层换号放大摘要重试。分片仍只解析一次、先计量再检查大小、复制原字节；只有完整正文才交付客户端。全局换号、WorkBuddy 短恢复和凭据业务仍待迁移。

M3 第六批将全局换号、单一总超时、租约生命周期及总尝试次数迁入 `gateway.RunAccountRetry`，同租约的有限恢复顺序归 `gateway.RunRecovery`。server 的 RetryDriver 保留原子准入、当前权限/预算、实际型号上下文及平台健康分类；WorkBuddy 仍独立判定 500/502/503/504、300ms 等待和 3s 进入窗口。短恢复消耗一个总尝试槽，不能重置总超时；窗口只限制进入，不能缩短生成时间。释放租约后才写健康结果；取消/请求级终止优先于平台分类，已提交输出在原有健康处理后禁止重放。准入锁、凭据刷新和账本没有随循环迁移。

M3 第七批将 WorkBuddy/Trae 的凭据解析、刷新互斥及提交协调迁入 `accounts.ProviderCredentials`，app 注入 Store 窄接口和实时续期开关。HTTP 仅选择适配器并单向委托；平台仍解释凭据和执行刷新，Store 仍是唯一条件提交方。正常路径仍只 Normalize 一次，不新增凭据 JSON 编解码或网络调用。`OperationLocks` 保留原有可取消按账号通道锁；凭据、领奖、成长任务使用三个独立实例，不能合并锁域。CodeArts 原生 Manager、IDE 会话读取和 DPoP 保持原实现，凭据表示及条件事务的后续迁移仍待独立验收。

M3 第八批将 WorkBuddy/Trae 分阶段登录、预览确认与凭据导入迁入 `accounts.ProviderLogins`。app 注入 Store 窄接口和原随机 ID 函数，当前适配器注册表逐次传入，不缓存第二份平台实例。HTTP 只解码 256KiB 参数、遮盖身份并映射原响应；平台仍负责设备/回调协议及身份核验，Store 仍是唯一原子保存方。保留 30s 超时、10 分钟会话、128 会话上限、单会话互斥、检查不保存、显式替换、保存失败可重试及删除后旧授权不能复活账号。登录业务不进入推理热路径；CodeArts 登录与账号管理其余业务仍待后续迁移。

M3 第九批将 CodeArts 浏览器登录的预览、确认保存、重复账号处理迁入 `accounts.BrowserLogins`；`upstream/codearts.BrowserLogin` 适配既有 LoginManager，负责 STS JSON 解析及身份核验。原生 PKCE/DPoP、回调校验、监听器、超时和单会话 Commit 锁不变；保存与失效仍在原 Commit 锁内，Store 仍是唯一原子保存方。轮询不入库、显式替换、失败可重试及删除后不复活保持原语义。普通 CodeArts Manager 凭据解析/刷新本批未迁移，不增加普通推理路径调用。

M3 第十批将 CodeArts 凭据的按账号互斥、短缓存、最新快照读取、条件提交和缓存失效迁入 `accounts.CredentialRuntime[T]`。原 Manager 内嵌同一个业务运行时并单向委托，保留零值、旧回调入口、互斥锁等待语义与不可变 Session 指针。平台仍负责默认策略、IDE 读取/回写、DPoP 刷新、代际内容比较和错误说明；Store 仍是唯一 CAS 提交方。缓存与提交顺序不变；失败不能恢复已轮转 token。该运行时不解释凭据，也不替换 WorkBuddy/Trae 的不透明 JSON 路径及可取消通道锁，不合并锁域。

M3 第十一批将账号增删、导入后的提交、并发设置、启停/暂停、清错、导出与重读协调迁入 `accounts.Management`。CodeArts 手工/桌面/JSON 凭据构造和原生导出留在 `upstream/codearts`，HTTP 保留 DTO、脱敏和状态码映射；app 装配同一原生 Manager。Store 原有严格事务与旧版仅记录保存错误的操作保持区别，缓存只在原时点失效，不增加锁域。普通推理及平台协议不进入这些管理方法。

M3 第十二批将摘要备用型号解析、同平台/地区约束、冷却检查与授权复核顺序迁入 `gateway.PrepareSummaryAttempt`，`requestpolicy.PrepareSummaryModel` 负责替换型号及清理原型号的顶层推理控制。server 只投影当前目录、策略和请求上下文，继续委托原权限/预算入口；Store 的准入事务、预算和原始型号白名单不拆分。首次准备不重复授权，同型号重试也必须复核；准备成功前不得开始新尝试。备用调用仍复用账号、凭据、期限和租约，不增加重试或扩展其他平台摘要能力。

M3 第十三批将分组目录归一化、缺省映射兼容、密钥引用及增删/启停、绑定和预算配置协调迁入 `access.Management`。五个持久化模型归 access，Store 保留兼容类型别名和原函数体；schema、原子权限发布、预算预留/重置、准入和账本不变。app 借用当前目录和设置，不新增缓存/锁；HTTP 保留 DTO、管理员守卫、脱敏和状态码。严格事务的失败回滚与旧版删除/启停仅记录保存失败的区别必须保留，不能借重构改变语义。该管理服务不进入推理路径；全局设置/模型策略、目录/测活、积分/成长任务和后台生命周期仍待迁移。

M3 第十四批将全局设置与模型策略的管理协调迁入 `config.Management`；Settings、ProviderSettings、PoolSettings、ModelPolicy 的规范定义与原纯校验/默认策略归 config，Store 保留类型别名和默认构造兼容入口。app 借用当前模型目录/别名；HTTP 继续负责解析、管理员守卫、脱敏与错误码，并在设置保存尝试后按原顺序应用身份头和限流。全局设置保存失败仅记日志、模型策略保存失败回滚的区别不变；密码哈希/会话失效、三平台局部更新、强制型号与分组映射冲突保持原规则。不新增配置缓存、锁或推理路径调用；目录/测活、价格/认证会话归属、积分/成长任务与后台生命周期仍待处理。

M3 第十五批将目录刷新编排、刷新状态缓存及有界测活迁入 `catalog.Runtime`，app 注入同一 Store 和 Registry，不创建第二份模型清单。HTTP 保留管理员守卫、原宽松解码、响应和任务锁取得时点；CodeArts 凭据/原生调用由 `upstream/codearts` 包装，WorkBuddy/Trae 继续借用原凭据和 chatOnce。刷新串行锁、缓存锁、测活锁各自独立；平台/地区分区、部分成功、失败保留旧目录/TTL、64 个模型上限、4 路并发、逐账号 30s 截止时间及探测参数不变。普通推理、目录解析/映射、账号探活、准入和计量不随本批调整。价格/认证会话、积分奖励、成长执行和应用/后台生命周期仍待处理。

M3 第十六批将积分查询/60 秒短缓存/四路批量查询迁入 `accounts.Credits`，中立 CreditInfo/CreditCategory 为唯一规范类型，CodeArts 保留兼容别名和原解析函数。`jobs.Rewards` 持有每日领奖的串行批量、按账号锁、等待期、确认及结果保存协调；HTTP 与后台定时循环单向委托，原平台协议由适配器提供。CodeArts 活动结果格式化留在 upstream 薄包装，旧原生保存失败仅记录日志、外部签到严格保存失败返回错误的差异不变。积分错误缓存例外、缓存失效时点、时区、显式停用账号语义及独立锁域均保持原行为；不扩大原生去重、不自动续期或领奖。成长执行、活动列表/邀请适配余项、价格/管理员会话和应用后台生命周期仍待处理。

M3 第十七批将成长任务清单/接取/领奖确认、活跃上报、Buddy 前置及付费动作编排迁入 `jobs.Growth`。原按账号可取消锁、两个执行槽、未知对话检查点及原子保存均由同一运行时持有，路径和 schema 不变；执行槽继续在 HTTP 写出后释放。server 保留管理员/平台守卫、严格解码、请求期限、锁内账号重读和凭据投影，付费回调仍走原预算、指定账号租约和用量账本。平台只提供既有可选能力；轮询次数/间隔、协议同意要求、未知写入不重放、失败关闭和缓存失效时点不变，不新增普通推理调用。活动列表/邀请余项、价格/管理员会话、应用后台生命周期及 M3 整体验收仍待处理。

M3 第十八批将活动列表与邀请的账号选择迁入 `jobs.Welfare`，CodeArts 原生查询/邀请及展示结果投影由 `upstream/codearts` 包装。HTTP 保留方法、宽松解码、默认活动 3、30s 请求期限、状态码和原错误前缀；列表与邀请的不同回退规则、显式停用账号、可领计数与自动领取判定保持原样。外部签到状态仍委托 Rewards，不新增邀请能力或真实领取；Store、平台协议、普通推理和页面不变，无新缓存、锁或网络重试。价格/管理员会话、应用后台生命周期与 M3 整体验收仍待处理。

M3 第十九批将价格值/校验与管理保存协调迁入 `usage`，Store.ModelPrice 保留类型别名及原严格事务；价格快照、历史费用、预算、账本和全部原计量函数体不变。`access.AdminSessions` 接管原内存摘要注册表和互斥锁，app 显式装配；保留 8 小时、128 项、最早过期淘汰、密码存储表示绑定、短路读取与恒定时间比较。HTTP 保留随机 token、Cookie、宽松解码、同源、失败限流、审计和状态码；无新锁域、清理协程、缓存或推理路径调用。密码哈希/恢复事务仍归原 Store，计量/存储拆分留 M5；应用后台生命周期、边界收尾与 M3 整体验收仍待处理。

M3 第二十批将 HTTP 运行实例及启动/关闭协调迁入 `app.Start`/`Runtime.Close`，原领奖定时循环归 `jobs.RunDailyLoop`；main 保留参数、导入/导出、首次访问配置、信号和致命退出策略。保留 8s 后首检/首检返回后启动 5min ticker、30s 保存、独立 60s 启动刷新，以及先 HTTP 最多 10s 排空、再最终保存、最后取消轮询的顺序。后台不新增 join、锁、重试或强制关闭，不能把本批当作修改退出保证的授权；server.StartClaimLoop 仅单向兼容。app.Build 仍无网络或后台副作用，普通推理/平台协议/Store/页面不变。M3 边界收尾与整体验收仍待处理。

M3 第二十一批将账号连通性探活编排迁入 `accounts.Probes`，CodeArts Session 和最小探测请求仍由 `upstream/codearts` 包装；HTTP 保留宽松解码、脱敏字段和响应。逐账号串行、30/60s 超时、原生先失效再解析、成功才替换平台地区目录、失败仍返回已有凭证提示、最终保存失败只记日志均不变。模型推荐生成/目录合并归 `config.ModelPolicies`，统一模型公共建议归 `config.CommonModelPolicy`，继续优先管理员策略并遇缺失来源立即停止；不按模型名猜能力，不增缓存/锁。`make architecture` 和 CI 检查生产直接导入，旧 Registry/apierr/Store 原生桥接明确保留，不代表整个依赖图已完成迁移。搜索执行、统一请求型号解析与 M3 整体验收仍待处理。

M3 已完成搜索执行、统一请求型号解析和整体验收，当前记录见 `docs/acceptance/STRUCTURE-M3-COMPLETE-2026-11-02.md`。以上各批是历史范围说明。catalog.Routing 借用唯一目录与实时权限；ModelPlan 仅本请求所有，不缓存分组授权，Store 仍原子选号/占位。gateway.RunSearch 保留平台与 SearxNG 的不同凭据/健康语义；protocol 只迁移原查询提取及型号外壳替换，不增普通请求解析。apierr 的业务错误仅单向别名。make architecture 覆盖直接与间接业务依赖；后续 M4 前端、M5 准入/存储和 M6 产品规则不能混入 M3 或同时改协议行为。

不要为凑齐目标树创建空包、空服务或无法运行的配置。具体职责和依赖见 `docs/architecture/`。

## 当前前端（M4 已完成）

前端源码在 `frontend/src`，Vue 预编译页面及独立原生 DOM 控制器各自独占节点。保持 `v-once` 所有权边界，不能与响应式 DOM 更新混用。旧 server/web 已删除，`internal/webui` 仅嵌入本次生成的 dist。新基础设施为 TypeScript，遗留控制器为 JS；具体规则见 `frontend/README.md` 和 ADR 0008。

先运行 `make frontend` 再执行直接 Go 命令。`make build/check/race/e2e` 自动构建必要资源；`build.sh` 包含锁定依赖安装、前端检查和构建。dist、node_modules、真实账号与浏览器测试产物不入库。API 类型从 `api/admin.openapi.yaml` 生成，`python3 tools/check_api_types.py` 检查同步。切页只取消读取，不能自动取消已提交写操作。

## 命令与验证

- 仓库根：`make check`、`make race`、`make build`、`make docker`；Go 不在 PATH 时通过 `GO=/absolute/path/to/go` 指定。
- Go 原生命令先进入 `backend/`：`go test ./...`、`go vet ./...`、`go build ./cmd/panel`。工具链版本以 `backend/go.mod` 为准。
- 根 `build.sh` 输出 `bin/codearts2api-panel`；`start.sh` 显式使用根 `data/state.json`，与调用者 cwd 无关。构建工具可通过 `CODEARTS_GO_BIN` 指定，版本通过 `CODEARTS_VERSION` 注入。
- 页面静态检查：根目录执行 `python3 tools/check_html_js.py`。启动器：`python3 -m unittest discover -s scripts -p test_connect_client.py`；Pi：`node scripts/pi/codearts-compaction.test.mjs`。
- 真实推理、领奖或共享凭据刷新按用户明确范围执行，常规回归使用隔离状态和假上游。
- 文档修改检查本地链接。历史验收中的原始命令和指标保留其当时上下文，当前运行说明使用新路径。

## 保持业务边界

账号权限复核、选号和并发占位维持同一原子入口，租约幂等释放。纯目录迁移不改变锁、重试次数、协议、请求参数和状态 schema。

凭据提交保留代际校验；已轮转 token 不允许用旧备份覆盖。结算账本和历史价格保持现有恢复保证，不用普通异步日志替代账本。

CodeArts、WorkBuddy、Trae 的平台协议相互独立。协议转换保留工具数据、大整数、合法拒绝和推理字段；不做全局文本清理。

## 运行与提交

修改服务前核对 PID、二进制、cwd、端口和状态参数。构建成功不代表已部署；模拟测试不代表真实上游通过。验收记录写明版本、环境、结果和未覆盖范围。

账号、密钥、refresh_token、访问恢复文件、运行状态和真实测试请求不进入 Git、镜像上下文或公开报告。提交说明沿用 `CONTRIBUTING.md`，第三方来源许可随文件迁移保留。
