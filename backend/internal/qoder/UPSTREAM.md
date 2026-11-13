# Qoder 原生适配器

Go 标准库实现，接入本项目的 Adapter / BrowserLogin，不启动 Qoder CLI、不执行其工具、不读取桌面凭据。协议资料核对日期：2026-11-03。

- 登录方式参考 [Qoder CLI Authentication](https://docs.qoder.com/cli/authentication)。官方文档中的 BYOK 是 Qoder 调用用户的 API，不是订阅账号推理端点。
- 设备授权、COSY 签名常量、编码和请求结构参考 [shuishuipingan/qoder2api-hub](https://github.com/shuishuipingan/qoder2api-hub/tree/090cfaedb12d17418af3cbd23a59a5cae529596d)。保留该项目的 MIT 声明：[LICENSE.qoder](../../../LICENSE.qoder)。不引入其账号调度、额外重试、系统提示词、工具执行或静态模型别名。
- 另核对 Liki4/qodercli2api 的协议说明，未复制其 AGPL 源码。

当前协议标识为 CLI 1.1.81。CN 与 global 的授权、账号和模型分别隔离。签名中的 RSA 公钥是服务端公开常量，不是账号密钥；访问/续期令牌保存在账号的不透明 credentials 中，由原有账号锁和 CAS 管理。

这是非官方数据面协议，可能随客户端更新。模型必须从当前账号的 chat 目录读取；发送实际 key，包含 business.model_key，不根据展示名猜测底层模型或能力。缺少 UMID 桥接时不伪造 MachineToken/Type。尚未完成真实 Qoder 账号验收，见 [使用说明](../../../docs/QODER.md)。

2026-11-07 R4 补齐结构化工具历史的空内容兼容：仅将带 tool_calls 的助手消息以及 tool 消息中的 null/缺失 content 转为 `""`。协议依据为上述固定版本 qoder_proxy.py 的 flatten_messages 结构化历史分支；没有采用其工具文本化、历史裁剪、推理补造或额外调度。保留实际历史、调用 ID、参数、推理和媒体。签名后请求检查及真实三协议复验见 [R4 验收](../../../docs/acceptance/QODER-TOOLS-R4-2026-11-07.md)；其中直接平台来源和隔离中继验证的范围分别记录，不代表所有型号、地区或账号均已验证。

## Campaign rewards (2026-11-08)

Billing protocol reference: [mmqz/cpa-multi-plugins campaign.go](https://github.com/mmqz/cpa-multi-plugins/blob/e3cd4bba5d6012354bb6a0d03c61b442ae8e93cf/plugins/qoder/campaign.go) and billing.go at the same revision (MIT, notice in LICENSE.qoder). Our small DailyRewards adapter uses plain Bearer, Cosy-ClientType 10, GET /sash/api/v1/me/campaigns and POST /sash/api/v1/me/campaigns/{id}/claim. Both CN and global use their existing region OpenAPI origin. The retired daily-check-in endpoint is not used.

Implemented within existing jobs.Rewards, without adopting the reference's separate scheduler, campaign ID cache/probing, device identity fallback, launch events or Pro upgrade flow. Only explicit credit benefits are accepted. Confirmation requires a fresh read of the submitted campaign ID. Tests use fake accounts and HTTP servers. On 2026-11-09, a user-run verifier confirmed claimable -> claimed -> already_claimed for one real Qoder account (advertised reward: 100 Credits); other accounts/regions and the deployed panel flow remain unverified.
