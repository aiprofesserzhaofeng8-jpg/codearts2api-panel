# 后端目录设计：上游按平台归档

审查基线：`b0c172f`（OfficeAce 已合入）。状态：设计已审查，平台尚未按本方案迁移。

新增渠道时，把只属于该平台的客户端、登录、模型发现、额度和协议实现放在 `backend/internal/upstream/<平台>/`，测试和来源说明随包放置。多个平台共同实现的接口归 `provider/`，在 `app/` 显式装配。维护者应能在一个目录里找到某个平台的实现，又能从导入关系看出哪些业务不能依赖它。

## 第一层：从维护任务找到包

下表是职责地图，不是要新建的父目录。沿着“登录一个账号”和“用它发送一个请求”两条路径看，比套一组 `controller/service/repository` 更容易判断改动应该放在哪里。

| 要改的行为 | 从哪里读 | 职责到哪里为止 |
|---|---|---|
| 服务启动、挂载管理和推理入口 | `app/` 装配依赖；`server/` 接收 HTTP，`webui/` 提供嵌入页面 | 创建平台客户端是启动工作，不在每次请求时重新注册。 |
| 用户能调用哪些模型、账号凭据什么时候刷新 | `access/` 管用户、密钥与授权；`accounts/` 管上游账号、登录协调及凭据刷新；`jobs/` 管后台任务 | 用户的 API 密钥与上游账号凭据是两类身份。某个平台如何登录、如何向上游换取凭据，归该平台。 |
| 改公开模型名、统一模型候选、组内可见性 | `registry/` 看清单和别名；`catalog/` 看发现、统一模型投影和路由；`config/` 看规则配置 | “有哪些候选”不是“本次占用哪个账号”。不要在模型目录中获取账号并发名额。 |
| 改准入、选号、提示词策略或重试 | `gateway/` 看执行编排；`scheduler/` 看选择算法；`requestpolicy/`、`ratelimit/` 看策略与限流；`storage/` 看原子状态更新 | 借用账号后，单次传输不能再选一次；不能把一次原子检查与更新拆成两次。 |
| 改客户端格式或某个供应商的网络交互 | `protocol/`、`responsescompat/` 看格式规则；`*wire` 看报文编解码；`provider/` 看能力契约；`upstream/<平台>/` 看供应商行为 | 只有该平台需要的签名、登录、端点、错误识别留在平台内。共用编解码不归第一个使用它的平台。 |
| 改价格、用量显示或账本持久化 | `usage/` 看统计、价格与费用计算；`storage/` 看持久化；`server/` 看接口如何返回 | 修报表不应改变已发请求的结算。旧 `store/` 是兼容入口，不是新功能的存储层。 |

## 第二层：哪些目录真的要移动

下图以 OfficeAce 为首批样板。WorkBuddy、Trae 表示后续归属，不表示首批一起搬。未列出的非平台包不移动。

```text
backend/internal/
├── app/                  # 创建客户端、注册平台并组装服务
├── provider/             # 平台标识、请求类型与可选能力接口
├── upstream/
│   ├── stream.go         # 根包：用已选账号绑定单次请求，不导入下方平台
│   ├── officeace/        # 首批：客户端、登录、模型、额度、测试和 UPSTREAM.md
│   ├── workbuddy/        # 后续：含该平台的签到、成长奖励实现
│   ├── trae/             # 后续：含该平台的登录和设备信息
│   └── codearts/         # 已有：适配层，继续借用既有签名客户端
├── codearts/             # 旧签名客户端；保留，不能与同名适配层直接合并
├── server/               # HTTP 入口和协议处理接入点
├── gateway/              # 准入、单次执行、重试等既有编排
├── registry/             # 模型清单与别名解析
├── scheduler/            # 调度算法
├── storage/              # 状态持久化与原子更新
├── protocol/             # 纯协议处理
├── chatwire/             # 以下为既有格式适配包，不并入纯协议包
├── messagewire/
└── responsewire/
```

其余待分批归档的平台：`antigravity`、`bedrock`、`chatgpt`、`claude`、`cline`、`commandcode`、`deepseek`、`gemini`、`grok`、`kimi`、`kiro`、`minimax`、`mobileclaw`、`opencode`、`qoder`、`teleagent`、`typesafe`、`vertex`、`zhipu`。具体批次按实际导入者、构建依赖和来源文件决定。共享传输包如 `providerhttp`、`cnapi`、`googlewire` 不因为被某个平台使用就归到该平台。

## 第三层：一个平台内部怎么排

OfficeAce 首批采用**一个 Go 包，按行为分文件**。下列是目标路径；文件名来自基线的 `internal/officeace/`。迁移只改变父目录。

```text
upstream/officeace/
├── client.go          # New、Describe：创建客户端、描述平台
├── login.go           # 发起登录、检查回调、交换授权码
├── auth.go            # Normalize、Verify：规范化并验证已有凭据
├── refresh.go         # Refresh：向上游换取新凭据
├── models.go          # 解析可信模型端点、发现模型、处理回退清单
├── chat.go            # Stream：发送模型请求，处理分片与平台错误
├── quota.go           # 读取额度、解释订阅和每日奖励状态
├── signer.go          # 平台请求签名
├── crypto.go          # 登录和签名所需的密码学辅助函数
├── client_test.go     # 客户端行为
├── endpoint_test.go   # 模型端点的信任边界
└── UPSTREAM.md        # 上游来源与实现说明
```

按改动选文件：**新增登录方式**先看 `login.go`，不是 `auth.go`；**修模型列表**先看 `models.go`，不是全局 `catalog/`；只有该平台返回的列表正确、但发布给用户的模型仍不对时，才沿目录投影往上查。WorkBuddy 的成长奖励和 Trae 的设备信息仍各归其平台，不要求每个渠道都建 `rewards/` 或 `device/`。

**什么时候再拆子包？** 只有某块逻辑能用窄接口独立测试、确有独立调用者或依赖隔离需求，才把它从文件拆成子包。同一客户端的几段代码共享私有状态时，按文件分开即可。不为制造层次而导出原本私有的凭据类型。

## 用一次 Messages 请求检验分层

以下沿基线的[Messages 入口](../../backend/internal/server/anthropic.go)和[原生转发分支](../../backend/internal/server/native_protocol.go)阅读，是**运行顺序**，不是 Go 包导入顺序。

1. **请求要求什么？** `handleAnthropicMessages` 在解析请求后调用 `resolveModelForRequest`，再按请求所需能力过滤统一模型候选。有些 Messages 请求要求原生能力，这会影响候选资格；不能先随意选号，再承诺一律转换。
2. **哪一个账号执行？** 流式与非流式入口都在现有 `withAccountRetry` 的单次尝试中获得账号。账号、并发名额和预算沿用既有准入路径；平台客户端不再建立第二个账号池。
3. **这个账号怎么发？** `tryNative` 在该账号上解析实际模型及协议。能用同协议上游则走原生路径；凭据刷新后再核对协议，前后不一致返回冲突。原生分支不适用时才进入现有兼容分支，不能把上游已经返回的错误当成“换个协议重发”。
4. **谁把结果交给客户端？** 平台产生响应分片，执行层观察尝试和用量，Messages 入口按客户端的流式或非流式约定返回。平台包不自行重新选号或结算；已有输出后也不能换账号从头再发。

例如，同一公开模型在两个账号上表现不同：**候选里没有该账号**，先查 `catalog` 的映射和能力过滤；**候选正确、却选不到可用账号**，查准入与调度；**账号已选定、上游报错**，沿实际平台的端点、凭据和传输去查。三个现象不应都归到“协议转换”。

## 审查结论：先补平台归属检查

[现有架构检查](../../tools/check_architecture.py)把 `provider` 及其子包视为中立业务代码，禁止依赖 HTTP 或具体平台；它明确区分中立的 `upstream` 根包与具体适配器。这是选用 `upstream/<平台>` 而不是 `provider/<平台>` 或通用 `pkg/` 的依据。

检查器目前用**路径最后一段**识别上游平台。用基线检查器构造生产导入边，得到如下结果（下表省略 `internal/` 前缀）：

| 导入关系 | 当前结果 | 迁移要求 |
|---|---|---|
| `gateway → upstream/officeace` | 拒绝 | 继续拒绝，包括经中间包间接导入 |
| `upstream/workbuddy → upstream/officeace` | 拒绝 | 继续拒绝 |
| `upstream/workbuddy → upstream/officeace/auth` | 放行 | 拒绝跨平台导入子包 |
| `upstream/workbuddy/login → upstream/officeace` | 放行 | 子包同样受所属平台的边界约束 |
| `upstream/codearts → codearts` | 放行 | 保留同平台已有桥接 |

首批 PR 在[现有规则测试](../../tools/test_check_architecture.py)里固化这些反例：对导入两端都按 `upstream/` 后的**第一段**判定平台，旧顶层路径仍按已知平台名识别。同平台内拆子包应能通过，迁移中途新旧平台之间也不能互导。不要靠删除 HTTP 或中立包的限制让构建通过。

## 首批 PR 如何落地

1. **以含 OfficeAce 的主线为起点。** 从 `internal/officeace` 整包移到 `internal/upstream/officeace`，保留包名、平台 ID、文件和测试。基线中旧路径的直接文本引用只有三处：`app/app.go` 的生产导入、`server/provider_login_service_test.go` 的测试导入、包内 `UPSTREAM.md` 的路径。注册仍由 `app` 完成。
2. **验证移动，而不是重做平台。** 运行架构规则测试、实际 Go 生产依赖检查及后端测试。用已有的登录、模型映射和路由用例核对行为：同一账号配置无需重录，同一模型与入站协议仍走原来的直通或转换路径。选号、凭据刷新、重试、流式输出与结算边界不随路径调整。迁移提交应可单独回退，无数据迁移。
3. **以真实产物确认路径完整。** 经现有 Linux、Windows 发布包验收；检查构建脚本、文档和许可清单对旧路径的引用。后续迁移 Bedrock、Gemini 等包时，[Linux 打包脚本](../../deploy/package_release.py)会从平台源码目录复制许可证和来源声明；改来源路径，保留产物内原有文件名和内容。仅通过 Go 编译不能证明发布包完整。

新增渠道时仍要按功能检查模型目录、配置和前端接入；平台已注册不等于允许模型映射。目录迁移不调整 `storage/model_mapping.go` 的能力名单，不合并 CodeArts 两层，也不拆分 `server`。这些改变若有必要，应有各自的行为依据和评审。
