# Gemini API Key 与 CLI / Code Assist 接入

本地源码已实现两种接入方式并通过隔离验收。**尚未使用真实 Google 账号验收，也未替换当前运行服务。** Pi 的签名回传限制仍存在，不能宣称所有客户端均可用。

## 添加账号

账号池 → 添加账号 → Gemini API / CLI，选择：

- **浏览器授权**：用于 Gemini CLI / Code Assist。生成 Google 链接，完成授权后粘贴包含 `code` 和 `state` 的完整 `https://codeassist.google.com/authcode` 回调地址；也可使用本次授权的 `code#state`。检查 Google 身份与项目后确认保存。检查阶段不保存账号，保存失败可重试，不会重复兑换授权码。
- **导入 → AI Studio API Key**：输入 API Key，校验目录后保存。不能拿 CLI token 当作 API Key。
- **导入 → CLI / Code Assist JSON**：用于已具备项目的完整凭据，必须包含 `access_token`、`subject`、`project_id`、真实 `expires_at`、`scope: "cli"`；`refresh_token` 可选但自动续期需要它。`subject` 必须与 Google userinfo 的稳定 ID 一致。导入会核验身份及项目，不能自行填写虚构身份。

新 CLI 账号只使用上游提供的默认套餐进行项目初始化；已注册但没有返回项目时，提示导入带项目的完整凭据，不遍历云项目后擅自替用户选择。初始化发起一次写请求，再有界轮询操作结果；未完成时可稍后检查。

原始来源明确区分：`gemini/api/{型号}` 和 `gemini/cli/{型号}`。刷新目录后由现有机制按精确名称归并，已有停用选择及分组权限保留。AI Studio 静态密钥无假过期时间；CLI 的新令牌由既有凭据协调器条件保存，不另建刷新服务。

## 目录、生成与配额

| 功能 | AI Studio API Key | CLI / Code Assist |
|---|---|---|
| 模型目录 | 原生 `/v1beta/models` 分页，读取真实输入/输出 token 限额 | 核验身份与项目后使用固定 Sub2API 参考目录；**不是实时账号权限清单**，不猜测上下文/工具能力 |
| 推理 | Google 官方 `/v1beta/openai/chat/completions` | `cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse`，携带 model/project/request |
| 认证 | API Key | Google OAuth + 项目绑定 |
| 续期 | 不需要 | 支持 refresh_token，保留未轮转值；并发刷新由现有协调器串行，单次条件保存 |
| 余额/配额 | 未提供可用余额查询 | `retrieveUserQuota`，按型号/类型显示剩余数量、百分比和重置时间；缺失保持未知，不伪造总额或美元余额 |

CLI 模型参考目录中的型号不保证该账号有权调用；一次文本请求也不能证明支持所有图片、工具或最大上下文。目录刷新、配额查询均不放进正常生成请求路径。

CLI 原生适配包含文本、system、内嵌图片、函数工具与结果、思考文本、签名元数据及输入/输出/缓存/推理用量。工具 schema 复用固定 Sub2API 清理函数；大整数调用参数单独保留。工具在客户端执行，网关不执行工具或保存上传文件。

远程图片 URL、文件/音视频输入、图片生成及未实现的扩展参数不静默丢弃；当前 CLI 图片使用内嵌 base64。空流、断流、业务错误和异常终止不能作为成功回复。保持型号，不用伪签名或自动换模型补救。

## 客户端限制

Chat Completions、Responses、Messages 的网关工具元数据往返已通过隔离测试，包含后两者流式/非流式多轮工具。依赖客户端保留工具项的 `extra_content`（其中可能有 `google.thought_signature`）。

本机 Pi 1.4.0 OpenAI-compatible 通路实测会丢弃该字段，所以 **Pi 的 Gemini 签名工具链路仍未通过**。切换到 CLI 上游不会自动修好客户端；未增加签名数据库、伪签名或静默换模型。当前相关模型工具能力仍保持未确认。

API Key/CLI 的真实 Google 授权、真实推理、识图、工具、配额和持续续期尚需用户自己的账号完成验收。不要把凭据发到聊天中。

参考与证据：[P2b 验收](acceptance/SUB2API-PLATFORM-P2B-2026-11-15.md)、[签名与 Pi 验证](acceptance/GEMINI-SIGNED-TOOLS-2026-11-15.md)、[Sub2API 复用及修改记录](../backend/internal/gemini/UPSTREAM.md)。

官方协议依据：[Google OpenAI 兼容文档](https://ai.google.dev/gemini-api/docs/openai)、[原生模型目录](https://ai.google.dev/api/models)、[固定 Gemini CLI Code Assist 实现](https://github.com/google-gemini/gemini-cli/blob/4ed06ed0ddc523a6d356a4ad35ddfea331041888/packages/core/src/code_assist/server.ts)。

## OAuth 客户端配置

当前源码和发布包不再内置 Google OAuth Client ID/Secret。浏览器登录和自动续期需要管理员提供获准使用、且符合该平台回调地址与 scope 要求的客户端配置；不能假定任意新建 Google OAuth 应用都有 Code Assist/Antigravity 权限。Gemini AI Studio API Key 模式不受影响。

部署环境变量：

```text
GEMINI_CLI_OAUTH_CLIENT_ID=
GEMINI_CLI_OAUTH_CLIENT_SECRET=
ANTIGRAVITY_OAUTH_CLIENT_ID=
ANTIGRAVITY_OAUTH_CLIENT_SECRET=
```

只填写使用的平台对应两项。Docker Compose 通过私有 .env 传入；systemd 可用 `sudo systemctl edit codearts2api` 添加 `[Service]` 下的 `EnvironmentFile=/etc/codearts2api/oauth.env`，该文件由 root 持有并设置 0600，填写变量后重启服务。直接运行则在进程环境设置变量后启动。不要把实际值提交仓库或发到聊天中。

原有 OAuth 用户令牌没有被删除，但续期依赖匹配的客户端配置；更换 OAuth 应用后可能需要重新登录。缺少配置时会明确报错，不尝试使用隐藏的默认值。
