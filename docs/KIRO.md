# Kiro 上游

Kiro 作为独立平台接入现有账号池、分组统一模型、公共/平台提示词和三个客户端接口。协议实现位于 `backend/internal/kiro`；协议来源和许可见 [UPSTREAM.md](../backend/internal/kiro/UPSTREAM.md)。不启动官方 CLI，也不执行模型返回的工具。

## 添加账号

管理员进入「账号池 → 添加账号 → Kiro」。

- **浏览器授权**：首版使用 AWS Builder ID 设备授权。选择 `us-east-1` 或 `eu-central-1`，生成链接，在官方页面确认，再回面板检查、确认保存。检查只产生预览，不自动保存账号。不需要手工回填回调地址。Google/GitHub 社交登录和企业自定义 Start URL 暂无浏览器流程，可导入完整凭据。
- **凭据导入 / API Key**：粘贴自己的 Kiro 凭据 JSON，面板先核对模型目录访问，再保存。不会读取其他项目或桌面客户端的凭据。API Key 示例（占位值无效）：

```json
{"api_key":"ksk_REPLACE_WITH_YOUR_KEY","region":"us-east-1"}
```

API Key 不经过 OAuth 刷新，也不伪造一个远期过期时间。面板显示「长期密钥」，实际是否过期/撤销由上游决定。不同 Key 是独立凭据记录；Key 更换时重新导入。

已有 OAuth 凭据接受 snake_case 或对应 camelCase：

```json
{
  "access_token":"REPLACE_WITH_ACCESS_TOKEN",
  "refresh_token":"REPLACE_WITH_REFRESH_TOKEN",
  "expires_at":"2099-01-01T00:00:00Z",
  "auth_method":"social",
  "region":"us-east-1",
  "auth_region":"us-east-1",
  "profile_arn":"arn:aws:codewhisperer:us-east-1:123456789012:profile/REPLACE"
}
```

日期只是格式示例，必须填令牌的实际有效期，不能延长真实凭据寿命。`expires_at` 接受 Unix 秒/毫秒或 RFC3339。Social 凭据需要 profile ARN。IAM/Builder ID 的 `auth_method` 为 `idc`，需要 `client_id`，自动续期还需要 `client_secret` 与 `refresh_token`；可以保留 `client_secret_expires_at`。`auth_region` 应使用原发令牌地区，不应因为改变数据地区而改动。未填数据地区时优先从 profile ARN 读取，否则默认 `us-east-1`。

账号标识来自 profile ARN；没有 ARN 的 IdC 凭据按 OIDC client ID 区分，重新注册客户端可能产生另一条记录，不能假定按邮箱自动合并。面板不从不可信邮箱字段推断账户归属。

## 模型与调用

保存后刷新目录，使用**目录中实际出现的完整 ID**：`kiro/us-east-1/<modelId>` 或 `kiro/eu-central-1/<modelId>`。没有凭据时不预置 Kiro 模型，也不保证某个订阅一定能用某个型号。

继续使用既有 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`，支持流式与非流式。示例：

```json
{"model":"kiro/us-east-1/<目录中的modelId>","messages":[{"role":"user","content":"你好"}],"stream":true}
```

「账号分组」可以把 Kiro 与其他平台的**兼容型号**映射到同一个带版本号的公开名称，由已有账号池调度。平台地区与原始型号白名单仍生效；公开名字相同不代表底层型号相同，管理员需要确认实际模型版本和能力。原始 ID 的隐藏与调用权限保持独立。

「配置 → Kiro」默认继承公共提示词，可以逐项覆盖。最终选中 Kiro 账号后应用 Kiro 策略。

## 首版协议边界

- 保留 system / developer 文本、历史、工具定义/结果、工具参数及大整数。Kiro 没有独立 system 字段，系统/开发者文本以明确角色标记放入首个用户输入，不保证与原生 system 权限完全等价。末条助手预填暂不支持。
- 图片仅接受受限大小的 base64 data URL，不由服务器读取远程图片地址。不支持音频、视频或任意文件内容块。
- 工具选择支持 `auto` / `none`；强制指定工具、禁止并行工具、结构化 JSON 输出、显式思考控制（包括禁用）、temperature/top_p/stop/seed 等无已验证原生控制，明确返回 400。默认自动选择工具不会执行任何工具，由客户端自行处理返回的工具调用。
- Kiro 原生数据请求不提供 `max_tokens` 控制。为兼容 Anthropic 必填字段和现有测活，该参数接受但**不保证执行**，实际输出预算以上游为准；有严格输出上限要求时不要依赖此路径。
- 不从模型名称推断上下文窗口、思考能力或价格。仅使用目录返回的元数据。
- 已接入 Credits 额度查询；签到和独立联网搜索暂未接入。未知 token 用量不会当作免费调用。设置 USD 预算时仍需按现有规则配置模型价格。
- 网络重试、并发、分组权限、取消、租约和结算沿用原有网关；不另建调度器。二进制帧损坏、工具参数不完整或上游异常会终止，已输出的部分不会换号重放。

## 验证范围

本地测试使用隔离状态及模拟上游，不读取现有账号、不消耗真实额度。覆盖设备授权与轮询间隔、预览确认、管理员权限、API Key 无到期刷新、OAuth 轮转、目录分页、两地区与三个客户端协议、工具和大整数、统一映射/白名单、计量/租约、损坏帧/取消，以及账号/用量筛选和提示词配置页面。

真实 Kiro 登录、API Key 调用、模型生成及自然到期续期尚未验收。源码修改不等于已经更新正在运行的系统服务或 GitHub Release。

## 额度与用量

已接入同地区只读 getUsageLimits，支持 OAuth 与 API Key。显示套餐、有效试用和赠送 Credits，保留精度与上游重置时间；未返回的数据不补为 0。查询缓存与不支持项见 [账号额度](ACCOUNT-QUOTAS.md)。
