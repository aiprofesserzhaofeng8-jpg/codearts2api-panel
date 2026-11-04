# ChatGPT / Codex 与 OpenAI API 接入

源码提供独立的 `chatgpt` 适配器。ChatGPT 订阅 OAuth 使用 Codex Responses 通道；官方 OpenAI API Key 使用 OpenAI Responses 通道。两种凭据不能混用，订阅额度与 API 账单也不是同一账户余额。

当前验证为隔离模拟上游、完整服务请求链和浏览器回归；尚未使用真实账号验收。编译成功、目录返回成功均不等于某个模型已获得推理权限。本次未改动本机或远端正在运行的服务、真实状态文件，也未发布 Release。

## 添加账号

在管理员「账号池 → 添加账号」中选择 **ChatGPT / Codex**，地区固定为 `global`。

浏览器方式：生成链接，在官方页面登录并授权；跳转后复制完整的 `http://localhost:1455/auth/callback?code=…&state=…` 地址，粘贴回面板，检查预览，再确认保存。面板不监听本机 1455 端口，因此浏览器可能显示连接失败；此时仍可复制地址栏中的回调地址。远程部署也采用这一方式，不需要把服务器回调端口暴露到公网。

授权使用 PKCE S256、随机 state，10 分钟过期。回调域名、路径、state 必须匹配本次授权；检查不保存账号，确认才保存。已兑换的授权码在会话内缓存，不重复兑换；关闭或取消会话后不能再提交。

导入方式支持 Codex `auth.json` 的 `tokens` 对象；也支持以下扁平 JSON：

```json
{
  "access_token": "你的 OAuth access token",
  "refresh_token": "你的 refresh token",
  "account_id": "ChatGPT workspace/account ID",
  "user_id": "ChatGPT user ID",
  "expires_at": 1794000000
}
```

示例时间仅表示 Unix 秒格式，必须替换为凭据的实际有效期。`account_id`、`user_id`、有效期可从 JWT 元数据读取；元数据仅用于路由和本地去重，不视为签名认证或网关权限证明。导入后访问上游模型目录验证凭据；缺少身份或有效期会拒绝保存。OAuth 可续期时由既有凭据协调器提交新代际，续期后不再访问模型目录或用户资料才保存。

官方 API Key 使用：

```json
{"api_key":"sk-你的官方OpenAI密钥"}
```

静态 API Key 不伪造过期时间，不执行 OAuth 刷新。这里不支持自定义第三方 base URL；凭据只发往适配器内固定的官方端点，不跟随重定向转发。

## 模型与调用

目录来源：OAuth 的 `GET https://chatgpt.com/backend-api/codex/models`，API Key 的 `GET https://api.openai.com/v1/models`。只使用返回的真实型号，不按名称臆测能力，不预置或替换现有 81 个模型。OAuth 目录请求使用协议版本参数 `0.144.0`，对应所参考源码的最低契约；不伪装官方 CLI 的 User-Agent。

原始模型 ID 为 `chatgpt/global/<真实目录型号>`。可在账号分组中给来源配置带版本号的统一名称；原始型号隐藏与允许直接调用仍由既有分组规则分别控制。

对外复用现有 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`，全部支持流式或服务端聚合的非流式返回。使用网关签发的 API Key，不要把上游 token 发给普通用户。

```bash
curl -fsS http://127.0.0.1:10087/v1/chat/completions \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"chatgpt/global/<真实目录型号>","messages":[{"role":"user","content":"你好"}]}'
```

请求中的型号占位符须先从面板模型页复制替换。相同分组可同时包含其他平台账号；只有显式映射到同一公开名称的来源才参与该名称的账号池调度。

## 兼容范围

- 文本、用户图片、普通 function 工具、工具结果、JSON Schema、大整数工具参数、文本拒绝和推理摘要。
- Responses 流须收到 `response.completed` 或带有效截断原因的 `response.incomplete`。EOF、流内错误、未闭合工具参数不会被当成成功。
- OpenAI API Key 的 `max_tokens` / `max_completion_tokens` 转为 `max_output_tokens`。Codex 通道不发送此限制，因为该通道不接受这一参数；客户端应使用网关总超时和预算控制，不能认为此参数限制了订阅通道输出。
- Codex 不支持本适配器的 `temperature` / `top_p`；明确返回 400。`stop`、`seed`、惩罚项、logprobs 和 `n != 1` 同样拒绝，不静默假装生效。
- 不支持音视频、电脑操作、上游内置搜索/代码执行工具、托管会话或 `previous_response_id` 连续状态；客户端应发送完整对话。本适配器不运行 Codex CLI 或本地工具。
- Claude 原生 signed thinking 不会映射成 Codex 思考。对跨平台统一名称使用原生 Claude 思考时，候选会限定到 Claude。
- OAuth 已接入使用窗口、重置时间及上游返回的额外 Credits 余额；API Key 显示不支持订阅窗口查询。独立搜索未接入。用量使用上游回执，USD 按管理员参考价计算，不是 OpenAI 账单查询。见 [账号额度](ACCOUNT-QUOTAS.md)。

## 实现与参考

`backend/internal/chatgpt` 负责凭据、官方端点和 Responses 转换；`backend/internal/providerhttp` 只提供有界 HTTP/SSE 与 PKCE。账号池、权限、提示词、刷新互斥、CAS 保存、重试和计量沿用现有模块。

认证与订阅转发参考 [Sub2API 固定版本](https://github.com/Wei-Shaw/sub2api/tree/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/pkg/openai) 和同版本的 `openai_codex_models_service.go`。这里为独立适配实现，没有复制其服务、调度器或身份伪装代码。

协议边界核对 [OpenAI Codex 认证文档](https://developers.openai.com/codex/auth) 和 [Responses API](https://platform.openai.com/docs/api-reference/responses)。这不是新版 Sign in with ChatGPT 的动态客户端注册、nonce/JWKS 流程，不宣称已实现该流程或获得第三方客户端的官方接入资格。订阅是否允许相应请求、实际权益与限流由上游决定；上游错误会返回网关，不通过伪装 CLI 绕过。
