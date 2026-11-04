# Claude / Anthropic 接入

独立的 `claude` 适配器支持 Claude OAuth 和官方 Anthropic API Key，两者最终都使用原生 Messages 协议。它们不是同一计费方式；模型可用性、订阅额度与 API 费用均以上游账号实际权限为准。

已通过隔离模拟上游与服务请求链回归，真实 Claude 账号仍未验收。本次未修改正在运行的本机/远程服务或真实状态，也未发布版本。

## 添加账号

管理员进入「账号池 → 添加账号 → Claude」。地区固定为 `global`。

浏览器方式：生成官方授权链接，登录授权后粘贴页面给出的 `code#state`，或者完整 `https://platform.claude.com/oauth/code/callback?code=…&state=…` 回调地址。检查账号预览后确认保存。授权使用 PKCE S256、随机 state 和 10 分钟会话，不接受不带 state 的裸授权码。

也可导入：

```json
{
  "access_token":"你的 OAuth access token",
  "refresh_token":"你的 refresh token",
  "account_uuid":"你的 Claude 稳定账号 UUID",
  "expires_at":1794000000
}
```

时间示例须替换为实际 Unix 秒有效期。支持 `claudeAiOauth` 嵌套导出中的 `accessToken`、`refreshToken`、`expiresAt`（毫秒会转换），但仍须提供 `account_uuid` 或 `accountUuid`。不会用会轮转的 refresh token 哈希代替稳定身份。导入会查询官方 OAuth profile 核对 UUID，再查询模型目录；不能只凭任意自填 UUID 保存。

官方 API Key：

```json
{"api_key":"sk-ant-你的官方Anthropic密钥"}
```

API Key 采用 `x-api-key`，不伪造有效期，也不调用 OAuth 刷新；OAuth 使用 `Authorization: Bearer`。不支持自定义中转地址、浏览器 Cookie、sessionKey 抓取或自动读取客户端密钥。

## 模型和外部接口

目录从 `GET https://api.anthropic.com/v1/models` 动态读取，按 `has_more/last_id` 分页，最多 20 页；异常游标或超过上限会失败，不发布半份目录。原始模型名为 `claude/global/<真实目录型号>`。不根据名称猜上下文、工具能力或创建模型别名。

复用现有 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`，支持流式与非流式。普通用户使用网关 API Key，平台 OAuth / API Key 只存于管理员账号池。

```bash
curl -fsS http://127.0.0.1:10087/v1/messages \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"claude/global/<真实目录型号>","max_tokens":1024,"messages":[{"role":"user","content":"你好"}]}'
```

先从面板模型页复制实际型号替换占位符。分组中可给多个来源建立带版本号的统一名称；模型菜单仅展示公开名称，原始 ID 的直接调用仍受分组白名单和账号归属控制。

## 兼容边界

- 支持文本、HTTPS / base64 用户图片、function 工具、工具结果、大整数参数、拒绝回复、上游缓存用量。
- 原生 `/v1/messages` 的 `thinking`（enabled/adaptive/disabled）、`output_config`、历史 signed thinking 与 redacted thinking 在 Claude 路由内保留；工具参数不会转成浮点数。签名按内容块保存，非流式 Messages 返回原始思考块，流式返回 signature_delta；不会给其他平台注入 Claude 签名。
- 对统一名称发送 Claude 原生思考或历史签名，会只保留当前分组允许的 Claude 来源；没有 Claude 来源就拒绝。普通文本仍可按映射的各平台账号池调度。
- Chat Completions 的 `reasoning_effort` 只映射 none/low/medium/high/max；它与原生 thinking/output_config 不可混用。是否支持 adaptive/effort 由实际模型判断，上游不接受时会报错，不按模型名猜测。
- `max_tokens` 默认 4096，显式正整数优先；不推测模型上限。`max_completion_tokens` 也可转换。`stop` 转为 `stop_sequences`，工具选择与禁用并行选项会转换。
- SSE 必须收到有效 message_delta 和 message_stop，所有工具块必须闭合且参数为完整 JSON 对象。中断、错误、缺失签名或未知不支持的内容块不会伪造成功。
- 目前是普通工具与文本/图片兼容通道，不等于完整 Claude Code 客户端。内置 server tools、电脑操作、音视频、文档块、pause_turn 等未接入；跨协议不会保留所有缓存控制/引用元数据。不执行本地 CLI 或工具。
- API 版本头为 `2023-06-01`；OAuth 增加 `oauth-2025-04-20` beta。不会覆盖用户提示词来假装 Claude Code，不伪造官方 CLI User-Agent 或工具名。上游可能因此拒绝第三方 OAuth 请求；真实订阅访问仍须实测。
- OAuth 已接入订阅短窗/周窗及重置时间，API Key 不冒充订阅窗口；见 [账号额度](ACCOUNT-QUOTAS.md)。每日奖励和独立搜索未接入。缓存读取按现有价格配置计费，缓存写入包含在普通输入计量中，尚无独立缓存写入价格。

## 结构与来源

`backend/internal/claude` 只拥有认证、目录、Messages 转换与流解析；账号池、分组、提示词、凭据条件提交、预算、租约和账本沿用现有实现。refresh token 轮转后不追加 profile 或目录查询才提交；已轮转凭据不因保存失败恢复旧值。

认证端点与字段参考 [Sub2API 固定版本 OAuth](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/pkg/oauth/oauth.go) 及同版本 `claude_oauth_service.go`；新增适配器为独立实现。Messages/SSE 和模型目录核对 [Anthropic 官方流式文档](https://platform.claude.com/docs/en/build-with-claude/streaming) 与 [Models API](https://platform.claude.com/docs/en/api/models/list)。参考源码不等于官方第三方 OAuth 支持承诺。
