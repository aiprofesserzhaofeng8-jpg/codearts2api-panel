# 账号额度与用量

账号池的「额度与用量」按上游实际单位展示。积分余额、订阅使用百分比、API Token 用量和本地 USD 预算是不同数据，不相互换算。

| 平台 | 查询方式 | 展示 |
| --- | --- | --- |
| CodeArts / WorkBuddy / Trae | 原有统计接口 | 保留原有积分/套餐展示 |
| Qoder CN / global | `GET /api/v2/quota/usage` | 套餐 Credits、附加 Credits、可用组织共享额度；分别保留精度和上限 |
| Kiro OAuth / API Key | 同地区 `GET /getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST` | 套餐 Credits、有效试用及赠送 Credits；套餐重置时间与包到期时间分别展示 |
| ChatGPT / Codex OAuth | `GET /backend-api/wham/usage` | 主/次及附加使用窗口、使用比例、实际窗口时长、重置时间；返回时显示额外 Credits 余额 |
| Claude OAuth | `GET /api/oauth/usage` | 5 小时、7 天及上游确实返回的细分周窗口、使用比例和重置时间 |
| OpenAI / Anthropic API Key | 当前没有订阅窗口查询 | 显示「暂不支持查询」，不冒充订阅额度或零余额；到官方 API 控制台查看实际用量/账单 |

ChatGPT 的 primary / secondary 不硬编码成短窗/周窗，标签由 `limit_window_seconds` 决定。Claude 仅在对应字段确实包含利用率时展示窗口；null/缺失不补成 0%。同一个账号的短窗和周窗彼此独立，不相加。

「已用 0%」需要上游明确返回 0；缺失数值显示未知。超过 100% 的实际利用率保留，进度条仅在视觉上封顶。重置时刻过后提示刷新，不在浏览器自行把用量清零。余额字符串保留原始十进制精度；较小的正用量显示 `<0.01`，避免被误看成 0。

Qoder `cap=-1` 的组织池只显示「额度未设上限」，不把占位 remaining 算成无限积分。上游全零且标记超限的退化响应不当成已确认的零余额；不使用含义未确认的顶层 expiresAt 推算积分过期。Kiro 的过期/停用试用和赠送包不计入有效额度，不从模型消耗 Token 推算 Credits。

Kiro 的语义资源与底层单位是两个字段：Credits 可以表现为 `resourceType=CREDIT`、`unit=INVOCATIONS`。解析优先识别资源类型，保留精度字段；多种资源同时返回时优先选择明确的 Credits 记录，不将其它资源或含义不明的调用次数换算成积分。该组合的原始响应示例见 [CodexBar 的 Kiro 复现记录](https://github.com/steipete/CodexBar/issues/3359)。

## 刷新与权限

保留既有管理员 `/admin/api/credits`：普通读取使用 60 秒缓存，「刷新额度」强制回源；单账号 15 秒截止，批量最多 4 路。需要续期时仍通过唯一的凭据协调器与条件提交，不另建 token 刷新路径。

额度查询仅使用只读上游接口，不生成模型请求、领取奖励、购买额度或消耗重置券。它只用于展示；查询失败不写推理账本，不按额度查询的 401/403 暂停整个账号或清空余额，不引入第二个调度/预算缓存。现有 API Key、分组与本地预算规则保持其职责。

新增返回字段为 `kind`、`unit`、`updated_at`、`windows`、`balance`，原有积分字段保留，状态 schema 仍为 10。各窗口的数值使用可选字段区分未知与 0，显示重置时间时转换为浏览器本地时间。

## 协议依据与验收边界

窗口与端点参考 [Sub2API OpenAI quota](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/service/openai_quota_service.go)、[账号用量](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/service/account_usage_service.go)和 [Claude usage](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/repository/claude_usage_service.go)，没有复制其调度器、重置券消费或自动购买逻辑。

Qoder 字段核对 [官方 SDK 用量说明](https://docs.qoder.com/cli/sdk/cost-usage) 与既有 MIT 参考项目 `qoder_accounts.py`；Kiro 核对 [官方计费说明](https://kiro.dev/docs/billing/) 与已有固定 MIT 参考项目的 `src/balance/client.rs` / `model.rs`。ChatGPT 的订阅用量与 API 额度区别见 [OpenAI 官方说明](https://developers.openai.com/api/docs/guides/chatgpt-api-credits)。新增解析为本项目独立实现，保留已有第三方许可。

本次自动验收使用模拟上游和隔离状态，不读取安装实例中的账号凭据。实际返回是否包含每一类额度、区域网络、OAuth 权限和真实重置行为，仍需要在有管理权限的实例中验证；不能把模拟返回当作真实账户余额。
