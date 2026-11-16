# P3b Grok 分批验收

日期：2026-11-16。状态：**Build OAuth、续期与动态目录基础代码完成，完整平台接入未完成。**

## 本批交付

- 参考固定 Sub2API 提交 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484，复用 xAI OAuth 客户端、scope、回调、CLI 端点与身份请求头规则；保留来源摘要和 LGPL 文本。
- 固定授权地址 auth.x.ai，回调为 http://127.0.0.1:56121/callback；PKCE/state 复用现有会话机制，每次发送独立 nonce。没有新会话数据库或清理线程。
- access_token 只解析 sub/team/exp 元数据，**本地未校验 JWT 签名**，不能以 Normalize 成功作为授权或订阅证明。没有使用 ID Token 作为认证凭据。必须由 Build /models 接受令牌后才能返回已验证预览。
- 授权码成功兑换后缓存令牌，目录暂时失败可重新检查，不重复兑换一次性 code。
- 刷新保留账号和团队，保留或更新 refresh_token，不新增轮转后的可选网络检查。缺刷新材料明确要求重新授权。
- 来源范围为 build，与普通 xAI API Key 分开。模型来自固定 CLI 端点 /models，不制造 fallback、不将 gpt/claude 自动映射到 Grok。不根据型号名猜测图片/工具能力。
- 拒绝 API Key/SSO/password 混合凭据和自定义端点，不接入账号密码登录或验证码第三方服务。

## 本批验证

`go test -race ./internal/grok ./internal/providerhttp`、`go vet ./internal/grok`、架构边界与 `git diff --check` 通过。

覆盖：PKCE、state、nonce 独立、错误 scope、取消会话、目录失败后兑换去重、刷新材料轮转/缺省保留、subject/team 不变、到期时间不被延长、真实目录字段/去重、媒体型号排除、空/坏目录与 401/403/404/429/503 不隐藏、不跟随重定向、不回显敏感错误正文。

证据：`/tmp/codearts-p3b-auth-race.log`。所有网络均为本地假上游，未访问真实用户凭据。

## 剩余工作

1. Grok Build 原生生成路径、Responses/Chat 兼容、工具/图片/推理字段与 SSE 错误、取消、用量验收。
2. 配额/订阅窗口查询及未知值处理，使用现有 CreditInfo，不创建第二份账本或额度存储。
3. app 注册、前端授权与导入、现有保存事务和并发续期、共享名称与分组权限的完整网关验收。
4. 分发许可证、Linux/Windows 构建、全量及浏览器检查。
5. 真实 xAI 账号登录、模型可用性、工具、额度和正常续期验证。

本批未注册为可用平台、未新增前端入口，未部署、推送或发布。普通 xAI API Key 与网页登录 SSO 不能被本批 Build OAuth 的完成状态代替。

[来源与改动](../../backend/internal/grok/UPSTREAM.md) · [实施计划](../architecture/SUB2API-PLATFORM-EXPANSION-PLAN.md)

## 后续生成、配额与应用接入

本节替代上文“基础代码完成、未注册”的历史状态：**P3b Build OAuth 模式源码及隔离验收完成，真实账号验收仍待完成。**

- 接入原生 Build Responses 端点，保留完整上游型号、CLI 身份头、图片与函数工具历史、大整数，不照搬跨模型图像描述或别名替换。
- 现有 ChatGPT 的 Responses 编解码提取到 responsewire，双方复用；该包无认证、端点、存储或调度职责。补齐输出 reasoning_tokens 的计量投影，输出总量不重复增加；ChatGPT 回归通过。
- 周/月订阅窗口按独立单位、独立时间投影到既有 CreditInfo。缺值与零区分；无第二份额度表、套餐猜测或新增软限制。
- 注册平台、前端授权与 JSON 导入、目录共享名称自动同步、现有保存/刷新协调器均已接入。
- Linux/Windows 分发与 Dockerfile 加入 Grok 许可证和来源清单。

验证证据（均在本机临时目录）：

- `/tmp/codearts-p3b-stream-race.log`：适配器及 ChatGPT/providerhttp race 回归。图片→工具调用→工具结果→文本两轮；缓存/推理/输出用量；空/错误/截断流；客户端取消；订阅单位与重置时间；8 路并发只刷新一次并保存新令牌。
- `/tmp/codearts-p3b-gateway-race.log`：三个 API × 流式/非流式 × 原始/共享名称 12 组真实网关假上游用例；计量、分组拒绝与租约释放。
- `/tmp/codearts-p3b-final-targeted.log`：授权预览后保存失败重试、不重复换码；Grok 公共名称自动同步。
- `/tmp/codearts-p3b-browser/browser-results.json`：Grok、Antigravity、Gemini、官方 API 和账号创建五组浏览器回归。
- `/tmp/codearts-p3b-check.log`：全量 make check（前端/Go/vet/架构/部署测试）。
- `/tmp/codearts-p3b-panel` 与 `/tmp/codearts-p3b-panel.exe`：Linux 构建、Windows 交叉编译；未在 Windows 运行，未构建 Docker 镜像。

未实现普通 xAI API Key、SSO/密码登录、生成媒体、产品级/预付费/按量账单明细、加密推理重放。本批没有真实账号推理或续期验收，没有升级生产服务、推送或发布。
