# 复用 Sub2API 扩展上游平台实施计划

日期：2026-11-15。状态：P0 已完成；P1 实现及隔离验收通过、真实 API Key 验收待完成；P2a API Key 基础接入已实现，网关签名跨协议隔离验收通过，Pi 签名及真实验收待完成；P2b 实现与隔离验收完成，P3a 认证/初始化/续期/目录/配额/生成与面板接入隔离验收通过，真实上游验收待完成；P3b Build OAuth/续期/目录/生成/配额/前端源码与隔离验收通过，真实账号验收待完成；P4a Zen/Go 凭据、目录、额度、三协议生成、短推理验证、应用/面板及分发源码接入与隔离验收通过，真实账号验收待完成；P4b Command Code 适配器、三协议生成、月度窗口、应用/面板和分发已实现，隔离验收通过，真实账号待完成；P4c Cline 认证/目录/余额/Pass 窗口、模型级故障隔离及应用面板基础接入完成，加密推理跨协议透明扩展隔离验收通过，真实账号待验证，Pi Messages 已确认丢失扩展；P4d TypeSafe 原生入口/应用/导入面板基础接入及隔离测试通过，消费者预算/钱包/并发/型号白名单及取消释放隔离验收通过，独立目录按用户要求跳过，真实账号待完成；P4e Kimi/智谱个人版/MiniMax 国内套餐路径已实现，真实账号与 MiniMax 当前额度结构待验证；P5a Bedrock 商业区域 Claude 签名/目录/账号导入/网关接入已实现，真实账号待验收；P5b 服务账号、Gemini 生成、显式 model_ids 目录与账号 UI 已接入，真实云账号待验收；P6 第一批本地打包/安装/升级/Docker 隔离验收已通过，真实账号、Windows 原生与正式发布待完成。详见 [P0 复用清单](sub2api-platform-expansion/UPSTREAM.md) 和 [回归基线](../acceptance/SUB2API-PLATFORM-P0-2026-11-15.md)。本阶段未修改运行服务、账号或凭据。

## 1. 目标与范围

用户要求：能抄就直接抄。优先复用 Sub2API 已实现的协议、解析器和测试，必要时改包名、依赖和数据类型；不为形式统一重写成熟逻辑。不将“兼容某协议”“可通过其他平台调用同名模型”算作支持该厂商账号。

目标：在当前九个平台之外，增加 Gemini、Antigravity、Grok、DeepSeek、智谱、Kimi、MiniMax、TypeSafe、OpenCode Zen/Go、Command Code、Cline，共 11 个平台；另外增加 Bedrock、Vertex AI 两个云渠道。协议/API Key/订阅/OAuth 分别验收，不用其中一个模式成功替代全部模式。

完成后的日常流程：添加账号 → 校验凭据与可用型号 → 刷新目录 → 自动生成/补齐共享名称 → 客户端通过同一网关调用。管理员不必逐个手填统一模型；已有分组授权、停用来源和客户端策略不自动扩大或覆盖。

第一批交付只承诺四个国内厂商的官方按量 API Key 接入；Coding Plan 是单独的后续子任务，不能把购买普通 API Key 当成拥有订阅权限。

## 2. 参考版本和复用规则

参考仓库：https://github.com/Wei-Shaw/sub2api

固定提交：`530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484`。P0 已核对第一批 22 个来源文件的摘要、直接依赖与抽取范围，见机器可读清单；后续平台仍为复用候选，不声称整文件可以原样编译。上游根目录有 LGPL LICENSE，无 COPYING；迁移时须另行附带所需 GPL 文本。

- **直接复制优先**：纯协议结构、请求/响应转换、流式解析、模型别名/端点规则和相关测试；不作无收益改写。
- **抽取后适配**：绑定 Sub2API Account/Service 的认证、余额、目录和限流解析逻辑，抽取平台部分，接入本项目窄接口。
- **仅参考**：其调度、数据库事务、Redis、管理 API、后台循环和用户权限。复用本项目对应实现，不引入第二套所有者。
- 每组迁移保留上游相对路径、固定 SHA、原始文件/片段摘要、改动原因和适用许可证；通过本地 `UPSTREAM.md` 及机器可读来源清单记录。
- 上游根许可证为 LGPL-3.0，本仓库根为 Apache-2.0；复制代码不能简单改为本仓库许可证。沿用已有 Sub2API 代码的许可隔离方式，逐文件核对声明，保留 LICENSE/COPYING/版权和修改记录，并检查源码、Linux/Windows 包及镜像的许可交付。不能仅以写出处代替分发要求，也不在计划阶段宣称混合许可问题已全部解决。
- 后续更新优先比较固定上游文件差异，先运行迁移的测试，再合入安全/协议修正；不自动跟随 main 覆盖本地代码。

参考：[固定平台定义](https://github.com/Wei-Shaw/sub2api/blob/530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484/backend/internal/domain/constants.go)、[许可证](https://github.com/Wei-Shaw/sub2api/blob/530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484/LICENSE)。已有迁移样例：[modelmeta 复用记录](../../backend/internal/catalog/modelmeta/UPSTREAM.md)。

## 3. 候选源码映射

下列路径相对上述固定 Sub2API 仓库。成组列出的目录在实施时只迁移实际需要的文件，不整个搬运。

| 范围 | 已找到的候选代码 | 预计迁移方式 |
|---|---|---|
| 国内四厂商 | `service/cn_provider_balance_service.go`、`cn_provider_quota_service.go`、`cn_provider_probe_url.go`、`ratelimit_cn_providers.go`（均在 `backend/internal/` 下） | 抽取端点/余额/额度和错误分类；转发继续复用本项目协议层；进一步定位厂商路由分支 |
| Gemini API / CLI | `backend/internal/pkg/gemini/`、`pkg/geminicli/` | 优先复制类型、OAuth、模型及工具结果处理和测试；API 与 Code Assist 分开适配 |
| Antigravity | `backend/internal/pkg/antigravity/`、`service/antigravity_gateway_*.go` | 优先复制转换器和测试；剥离服务层调度/重试依赖 |
| Grok | `backend/internal/service/grok_*.go`、`openai_gateway_grok*.go` | 抽取认证、目录、额度、流处理及测试；不把其全部服务状态带入 |
| OpenCode | `backend/internal/service/opencode_go.go`、`opencode_go_usage.go`、`openai_opencode_session.go` | 复用 Zen/Go 端点及型号协议路由；保留已知不支持的型号/端点限制 |
| Command Code | `backend/internal/service/command_code.go`、`command_code_usage.go` | 复用多协议路由、套餐窗口和解析测试 |
| Cline | `backend/internal/service/cline.go`、`cline_usage.go`、`ratelimit_cline.go` | 复用认证/目录/积分及窗口规则，映射现有错误类型 |
| TypeSafe | `backend/internal/pkg/typesafe/` | 优先复制客户端和 System One 协议；不将它误作普通 OpenAI 端点 |
| Bedrock | `backend/internal/service/bedrock_request.go`、`bedrock_signer.go`、`bedrock_stream.go`、`gateway_bedrock.go` | 优先复用签名、事件流及测试，账号和区域归本项目适配器 |
| Vertex AI | `backend/internal/service/vertex_service_account.go`、`gemini_vertex_token_error.go` | 复用服务账号认证与区域/项目逻辑，复用已接入 Gemini 协议 |

## 4. 本项目落点与复杂度约束

1. 平台实现放在 `backend/internal/<provider>/`，接入现有 `provider.Adapter`：Describe、Normalize、Refresh、Stream、Models、Credit；只有真实支持浏览器授权的平台才实现可选登录接口。
2. 在 `internal/app` 装配；登录保存用 ProviderLogins，续期用 ProviderCredentials，目录用 catalog.Runtime，推理用现有 gateway/protocol，状态和结算仍由 storage 唯一写入。
3. 优先复用已有 providerhttp 和协议转换；只有两个真实实现出现重复时才抽共用代码，不预建“大而全兼容平台框架”。国内四厂商可以共用标准传输，但厂商端点、套餐及错误规则分别维护。
4. 账号凭据仍放平台私有 JSON，不向中立 Account 增加厂商字段。平台模式/区域属于账号元数据，不能暗中把一个账号换到另一套餐或供应商。
5. 模型来源继续使用 `平台/范围/完整上游型号`。普通 API 与 Coding Plan 若需区别端点/能力，实施前明确 scope 兼容方案；不能只按平台/地区混为同一种能力。
6. 模型发现沿用正常目录刷新；没有目录接口才用有出处的有限列表并标明依据。不从模型自述推断身份，不将 auto/路由槽强行并为原生型号。
7. 能力声明是该来源实际支持的能力；共享名称按既有规则计算。原生有图片能力不代表反代来源一定能处理，无法支持的输入明确报错，不静默丢弃或换型号。
8. 无原生余额/套餐接口时显示“未提供/未知”，不把套餐余量当美元余额，不制造总额/已用值。
9. 不增加文件保存服务、数据库、Redis、全局重试层、定时刷新系统或每次推理的目录/额度查询。固定可信上游端点，避免为本次平台接入顺便增加任意 URL 代理。
10. 前端沿用平台描述、现有添加账号流程和统一模型页面；提供准确的 API Key/OAuth/订阅选项，不给所有平台展示虚假的浏览器登录按钮。

## 5. 分阶段任务与完成标准

| 阶段 | 具体目标与交付 | 完成标准 |
|---|---|---|
| P0 复用准备 | 固定上游 SHA；逐文件确定直接复制/抽取/参考；建立来源及许可清单；记录现有九平台回归基线 | 第一批所需文件依赖可控、许可记录齐备，明确接入矩阵及来源 ID；不建空适配器 |
| P1 国内官方 API | DeepSeek、智谱、Kimi、MiniMax 四个 API Key 平台；目录、生成、余额（如有）、错误处理、UI 和自动归并 | 四个平台完成隔离端到端测试；有真实密钥的平台逐一实测；不存在模型发现成功就冒充生成成功 |
| P2 Gemini | P2a 官方 API Key；P2b Gemini CLI/Code Assist OAuth；原生内容/工具/用量转换 | 两种认证路径分别通过；多轮工具、图片、流式结束及刷新材料正确保存；保持 scope 区分 |
| P3 订阅反代 | P3a Antigravity；P3b Grok；分别迁移登录/续期、目录、配额和生成 | 每个平台独立交付，完成原生支持能力及身份保持验证，不借另一平台成功代替 |
| P4 聚合与套餐 | P4a OpenCode Zen/Go；P4b Command Code；P4c Cline；P4d TypeSafe；P4e 国内厂商实际提供的 Coding Plan | 每种可用认证/套餐单独验证；端点与模型协议匹配，错误及配额窗口正确；无权限的模式不算支持 |
| P5 云渠道 | P5a Bedrock；P5b Vertex AI；支持范围内的区域/项目、签名/服务账号认证 | 真实云账号才能完成实时验收；平台层签名、事件流及临时凭证隔离测试齐全 |
| P6 总体验收与交付 | 支持矩阵、权限/保存/共享名称回归、Linux/Windows/Docker 包、文档与 Release | CI/包/升级检查通过；发布说明逐平台列实现/模拟/真实/部署状态；未验证项不标全绿 |

实施顺序：P0 → P1 → P2 → P3 → P4 → P5 → P6。每个子平台独立收尾，不攒到最后才补测试。用户可要求先发布已验收平台，其余明确列为未完成；不能因此把全计划标完成。

## 6. 每个平台必须完成的验收

- **认证与状态**：格式合法不等于上游可用；验证身份或权限，保存失败不得假成功。API Key 不伪造刷新期限；OAuth 通过既有协调器保存，旧代际不得覆盖新代际。
- **目录与归并**：有效账号才形成可用来源；首次接入自动建公共名称，增加平台自动追加；已有停用选择及分组授权保留，名字接近但版本不同不擅自合并。
- **文本与流**：随机标记匹配、非流式与流式、终止事件、错误帧、超时/取消和并发释放；HTTP 200 但空响应或业务错误不能通过。
- **工具与多模态**：工具名/参数/ID/结果回传、多轮历史、图片合成识别；只测可信支持能力。文件和音视频若不在本批协议支持范围，明确列为未支持，不做本地落盘兜底。
- **计量与调度**：输入/输出/缓存/推理用量保留来源含义；无法获得用量标未知；一次逻辑请求不重复结算，在途归零。
- **错误与故障**：401/403/429/5xx、无权限、过期、配额耗尽、断流、刷新失败；不依据未知业务码永久封禁账号，不叠加无界重试。
- **客户端**：至少一个真实客户端调用共享名称，测试分组限定目标平台，日志核对实际来源；临时资源清理，不修改其他生产分组。
- **回归与打包**：每批 `make check`；凭据、共享目录、存储/调度修改跑针对性 race；UI 改动跑对应浏览器测试；新增前端 raw import 同步 Docker COPY、Windows 路径及安装包许可文件。
- **真实测试限制**：只用用户已有授权账号及有界合成请求，密钥在本机输入/读取，不要求粘贴进聊天；无账号、无订阅或上游拒绝时记为受阻，继续其他独立工作，不伪造通过。

验收表字段：平台、认证模式、来源 ID、共享名、协议、文本/流/工具/图片结果、用量、实际来源、保存/续期、运行版本、测试时间、未覆盖项。分为“未开始、实现完成、隔离验收通过、真实验收通过、已交付”，另记受阻原因。

## 7. 维护与发布

每个平台在 UPSTREAM.md 指向所复用的固定文件和本地改动；更新只同步相关协议/测试，不整仓替换。新增厂商型号先用上游目录，再走现有精确归并规则，人工只处理歧义和权限。

发布前审查凭据/私有状态排除、源码许可及制品许可，核对安装脚本、Windows/Linux/Docker 产物和升级恢复。运行服务升级与 GitHub 发布分别记录；没有部署证据不得称已经上线。首批实现不需要采购账号或更改当前生产套餐。

## 8. 当前状态与下一步

P0 和 P1 工程工作已完成，见 [P1 验收](../acceptance/SUB2API-PLATFORM-P1-2026-11-15.md)。四个国内官方 API Key 平台已实现并通过隔离测试，真实密钥验收待完成，当前服务未替换。当前 P2a 已加入 Gemini API Key、原生目录分页及官方 OpenAI 兼容端点；详见 [P2a 记录](../acceptance/SUB2API-PLATFORM-P2A-2026-11-15.md)。P2 尚未整体完成：CLI OAuth 与原生转换已完成隔离验收，Pi 签名回传及真实账号验收仍待完成；不再询问“是否允许参考/复制”，该偏好已明确。遇到实际许可、依赖或账户权限阻碍时说明具体问题，不能将未实现的部分包装成已接入。

### P2a 实施选择（2026-11-15）

核对固定 Sub2API Gemini 服务及工具/模型文件后，API Key 路径先使用 Google 官方 `/v1beta/openai/chat/completions`，原生 `/v1beta/models` 提供目录和 token 限额。复用已验收的 cnapi 传输，只增加固定 scope 字段（国内默认 cn 不变，Gemini 为 api）。避免将 Sub2API 的 Chat→Responses→Anthropic→Gemini 多层转换及服务依赖搬入现有网关。Google 的服务端兼容层不等于本项目原生 Gemini 转换；后者和 CLI OAuth 已在 P2b 以独立 cli scope 实现，见 [P2b验收](../acceptance/SUB2API-PLATFORM-P2B-2026-11-15.md)。

原始思考签名在 Chat Completions 流式透传、非流式聚合和工具历史回传中保留；Responses/Messages 的签名转换已通过网关隔离验收，但本机 Pi 会丢弃扩展字段，不能宣称所有 Gemini 客户端工具调用已完成。不加签名数据库、文件缓存或后台任务；后续优先复用固定上游已有协议代码。

### P4e 首批（2026-11-16）

Kimi 国内 Coding API Key 模式接入现有账号与模型流程；详见 [P4e 验收记录](../acceptance/SUB2API-PLATFORM-P4E-2026-11-16.md)。智谱个人版与 MiniMax 国内套餐已在第二批接入，真实账号验证待完成。按用户要求暂缓 TypeSafe 独立目录/页面，不作为 P4e 前置条件。

### P5a 首批（2026-11-16）

已迁移 Bedrock 官方 SDK 签名及有界 EventStream 解析，复用现有 Messages 转换。第二批已接入平台与账号入口，真实账号待验收；剩余项见 [P5a 记录](../acceptance/SUB2API-PLATFORM-P5A-2026-11-16.md)。

### P5b 首批（2026-11-16）

服务账号 JWT 换取令牌、项目/区域身份、固定令牌端点与 Gemini 区域/global 地址构造已实现。通过已有 ProviderCredentials 完成并发换取与持久化，不引入第二份令牌缓存或锁。尚未注册平台或开放面板；下一批接入 Gemini 原生生成、目录与导入验证。见 [P5b 验收](../acceptance/SUB2API-PLATFORM-P5B-2026-11-16.md)。

### P5b 第二批（2026-11-16）

已接通服务账号导入前换取令牌、短推理校验、Gemini 原生生成和既有账号/权限/共享模型/用量链路。当前目录为明确配置的 model_ids，不宣称自动发现；未加入 Vertex Claude、API Key Express 或 ADC。真实云账号验证待完成，当前运行服务未替换。使用说明见 [Vertex AI](../VERTEX.md)。

### P6 第一批（2026-11-16）

支持矩阵、实际 Linux/Windows amd64 打包、Linux 真进程安装恢复、独立 systemd 升级失败路径及 Docker 构建/Compose 验收通过。修复 Windows 原生验收许可清单过时问题。没有用模拟结果替代真实账号测试，没有推送或部署；详见 [P6 记录及剩余项](../acceptance/SUB2API-PLATFORM-P6-2026-11-16.md)。

### P6 第二批（2026-11-16）

arm64 交叉编译制品及完整摘要/ELF 检查通过。本机无 ARM 执行支持，Docker ARM 运行受阻；已补原生 ARM 可复用 CI 与 Release 依赖，正式发布使用该作业验收过的制品。工作流尚未推送运行，不能标记 ARM 运行通过；见 P6 验收记录。

### P6 第三批（2026-11-16）

实际 Pi 1.4.0 离线两轮验证：Cline Chat 扩展可回传，Messages 扩展丢失；Gemini Chat 工具签名丢失。Cline Responses 转换复验通过。新增可重复运行的客户端元数据诊断脚本并明确兼容边界；没有为客户端丢字段增加网关存储或伪签名，详见 P6 第三批记录。

### P6 第五批（2026-11-16）

兼容源码摘要检查接入 make check（离线）；固定上游来源与完整兼容回归接入 CI 和 Release（10分钟超时）。本地命令验证通过不代表远端 workflow 已执行。后续应转向真实账号验收和提交后的远端 CI，不以新增与交付无关的架构工作替代剩余验证。
