# Huawei Cloud OfficeAce (云果办 / AgentArts) protocol

`backend/internal/officeace` implements the Huawei Cloud OfficeAce (云果办 / AgentArts, provider ID `officeace`) upstream channel.

Protocol references and implementation verified against `/tmp/officeace2api` (MIT) and `/tmp/magpie-codearts-index.mjs` (OfficeAce provider implementation):
- `POST https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/state`
- `GET https://auth.huaweicloud.com/authui/login.html?service=...` (inner `/authui/v1/oauth2/authorize` with `client_id=pdp5_for_agentarts`, `code_challenge`, `code_challenge_method=SHA-256`, `state`, `scope=openid`, `redirect_uri=https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/callback`, `response_type=code`)
- `GET https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/auth/code?state=<state>` (202 polling, 200 code exchange)
- `POST https://sts.cn-north-4.myhuaweicloud.com/v1/oauth2/tokens` (`grant_type=authorization_code` and `refresh_token`, supporting `pdp5_for_agentarts` and `codearts-agent`)
- `GET https://agentarts.cn-southwest-2.myhuaweicloud.com/v1/claw/client-permission-validate` (SDK-HMAC-SHA256 signed V4)
- `GET https://officeace.cn-southwest-2.huaweicloud-agentarts.com/v1/subscription` (V11-HMAC-SHA256 signed regional scope `cn-southwest-2/apic`)
- `GET https://officeace.cn-southwest-2.huaweicloud-agentarts.com/v1/model-token-rates` (V11-HMAC-SHA256 signed)
- `POST https://officeace.cn-southwest-2.huaweicloud-agentarts.com/v1/subscription/bonus/claim` (V11-HMAC-SHA256 signed daily check-in)
- `GET https://api.modelarts-maas.com/v2/models` and `POST https://api.modelarts-maas.com/v2/chat/completions` (Basic base64(model_app_key:model_app_secret))

No proprietary client binaries or third-party private secrets are bundled.
