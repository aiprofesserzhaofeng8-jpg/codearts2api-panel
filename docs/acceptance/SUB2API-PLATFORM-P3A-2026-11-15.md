# P3a Antigravity 分批验收

日期：2026-11-15。状态：**基础客户端完成；平台接入未完成。**

## 本批完成

- 固定 Sub2API 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484，复制目录响应类型，适配 OAuth 客户端、scope、回调和端点，保留 LGPL 文本及摘要清单。
- 独立 Antigravity OAuth，复用现有 PKCE 会话；完整回调地址校验 state。兑换成功后缓存令牌，项目检查暂时失败重试不会再次兑换一次性 code。
- Google subject 与项目保持匹配；凭据归一化拒绝混用 Gemini CLI scope、API Key 或自定义端点。未返回项目时提示先在官方客户端初始化，不伪造项目。
- 刷新保留 subject/project；上游返回新 refresh_token 时保留新值，返回后由现有协调器负责保存，不增加刷新线程或存储。
- 同一固定 daily 端点调用 fetchAvailableModels，使用实际目录和窗口数据；保留上游型号、上下文、输出限制和图片字段。未声明的能力不猜测，特殊媒体生成型号不发布为文本接口。
- 配额 remainingFraction=0 表示耗尽；缺失字段表示未知，不能推算美元余额或捏造总额；重置时间按 RFC3339 解析。
- 401/403/429/503 和重定向明确失败，无私自切端点重试，不回显上游敏感响应。

## 已通过的隔离验证

`go test -race ./internal/antigravity ./internal/gemini ./internal/providerhttp`

- Antigravity OAuth state、PKCE 参数、下游暂时失败后的兑换去重、账号身份不匹配、取消授权。
- 旋转刷新令牌与稳定账号 ID；拒绝不匹配的 scope/混合凭据/任意端点。
- 动态模型字段及确定性排序；真实零值与未知窗口、非法比例/null 型号。
- 上游 HTTP 错误及禁止重定向；无错误正文泄露。
- 既有 Gemini/providerhttp 回归及 race 检查。

另通过新包 `go vet`、架构依赖检查及 `git diff --check`。

以上均为本地假上游，不是 Google 账号实测。

## 剩余交付

1. 原生生成转换、工具签名往返、图片、流终止、取消和用量；不照搬上游的自动换模型、身份提示词清洗及 dummy signature。
2. 登录预览/保存、续期 CAS、分组权限、目录自动归并与真实网关端到端测试。
3. 前端平台入口及浏览器授权操作说明、浏览器验收。
4. 将许可加入 Linux/Windows/容器分发，运行全量检查。
5. 真实 Google 账号登录、目录、生成与正常续期验证。

目前未装配到 app 注册表，未显示为支持平台；未修改生产服务、账号和凭据，未推送 GitHub 或发布版本。

源码及来源见 [Antigravity UPSTREAM](../../backend/internal/antigravity/UPSTREAM.md)。

## 2026-11-16 后续完成记录

本节取代上文“未注册”及剩余交付第 1～4 项的历史状态：**已注册并接入面板，隔离验收通过；真实上游及新账号自动 onboarding 未完成。**

- Gemini 与 Antigravity 共享 googlewire 原生内容/工具/流转换；认证和端点独立。已有 Gemini 回归通过。提取未新增存储或调度层。
- Antigravity 原生封装保留 model/project、agent 请求类型、User-Agent、独立 requestId、稳定首条用户内容 sessionId，按 Sub2API 增加必要的 toolConfig。无身份提示词替换、跨模型搜索回退或假签名。
- 三个外部协议 × 流式/非流式 × 原始/共享名称共 12 组网关用例；另验证 Messages/Responses 四组带签名工具往返。检查上游路由、缓存/推理用量、分组拒绝和租约归零。
- 适配器图片/工具/大整数、错误流、空流、断流、取消与客户端退出测试；8 路真实协调器并发测试只刷新一次，保存后的旋转令牌可重读。
- 增加 Antigravity 来源自动归并到已有 Gemini 公共名称的回归，保留三个独立来源。
- 中英文浏览器授权/预览/保存失败重试/导入测试；既有 Gemini、官方 API 和账号创建浏览器用例回归。
- Linux/Windows 包及 Dockerfile 包含 Antigravity、googlewire LGPL 文本和来源清单。归档测试验证附件与摘要；Docker 镜像未实际构建运行，Windows 为交叉编译，未在 Windows 运行。

证据位于本机临时目录：`/tmp/codearts-p3a-check.log`、`/tmp/codearts-p3a-race.log`、`/tmp/codearts-p3a-gateway-race.log`、`/tmp/codearts-p3a-browser/browser-results.json`。

**真实验证未完成**：未使用任何 Google 生产凭据或发送真实推理。Pi 签名丢失依然是客户端限制。新账号缺项目需先在官方客户端初始化，不能把浏览器入口已经存在表述为完整新账号 onboarding 已完成。没有升级正在运行的服务、推送 GitHub 或发布版本。

## 2026-11-16 新账号初始化补齐

本节取代前述“新账号自动 onboarding 未完成”的状态。**P3a 已完成源码与隔离验收，真实 Google 账号验收仍待完成。**

- 浏览器授权发现项目缺失时，复用 Sub2API 的默认套餐选择和 onboardUser 协议。只选择 allowedTiers 中 isDefault=true 且非空的 ID；没有默认值则明确失败。
- done=true 时必须取得有效项目；字符串和对象 id 两种格式均接受。已有项目不得变更。
- 仅明确 done=false 时按同一套餐继续请求，最多 5 次、间隔 2 秒，等待可取消。HTTP/网络错误、业务错误和不完整响应立即停止，不因未知结果自动重试写入。
- 导入验证、目录、推理和自动续期不执行初始化。无新增缓存、存储、定时任务或端点切换。
- 真实 ProviderLogins 协调器配合假上游/故障存储验证：授权及初始化完成后只生成预览；第一次保存失败明确报错，第二次保存成功；整个过程中只兑换一次授权码、只初始化一次。

本批通过：Antigravity/Gemini/providerhttp 的 race 回归（Antigravity 11.114s，含 5 次上限测试）、新包 vet、架构边界、Linux 打包许可检查、git diff --check。
证据：`/tmp/codearts-p3a-onboard-race.log`、`/tmp/codearts-p3a-onboard-package.log`。

未重复宣称真实验证：没有使用生产账号、没有升级运行服务、没有推送 GitHub。下一个工程阶段为 P3b Grok；P3a 的真实登录/初始化/生成/自然续期验收单独保留。
