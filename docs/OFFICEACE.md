# 华为云 OfficeAce（云果办 / AgentArts）接入

独立的 `officeace` 适配器支持华为云 OfficeAce（云果办 / AgentArts）原生反代，支持浏览器 OAuth 授权登录、凭据 JSON 导入、自动续期、动态模型目录、订阅/奖励积分查询及每日签到。

## 添加账号

管理员进入「账号池 → 添加账号 → OfficeAce」。地区固定为 `cn`（国内版）。

### 方式一：浏览器授权登录

1. 点击「生成授权链接」，面板在云端发起 OAuth 会话并生成华为云官方登录链接。
2. 复制链接到浏览器登录授权（支持华为云账号密码、手机验证码或微信扫码）。
3. 授权完成后回到面板点击「检查授权信息」，系统自动从云端轮询获取授权码并换取 STS 临时凭据与 MaaS 网关凭据。
4. 核对账号预览（显示账号 ID、手机号、有效期及是否支持自动续期），点击「确认保存账号」。

### 方式二：凭据 JSON 导入

支持导入已有的 OfficeAce / AgentArts 凭据 JSON（包含 `access_key_id`、`secret_access_key`、`security_token` 等 STS 临时凭据，以及可选的 `refresh_token` 与 DPoP 私钥）。导入前系统会调用云端 `client-permission-validate` 接口核对账号身份并获取专属 MaaS 网关与 Basic 认证密钥。

支持的 JSON 字段格式包括：
- 扁平或 `auth` / `account` / `access` 嵌套格式；
- 标准字段名（`access_key_id`、`secret_access_key`、`security_token`、`expires_at`、`refresh_token`、`dpop_private_key_jwk`）或历史驼峰字段名（`accessKeyId`、`secretAccessKey`、`securityToken`、`expiresAt`、`refreshToken`、`dpopPrivateJwk`、`modelAppKey`、`modelAppSecret`）。

## 凭据续期

- 凭据有效期通常为 24 小时；包含 `refresh_token` 与 DPoP 密钥对的账号在过期前 5 分钟内由网关使用 `POST https://sts.cn-north-4.myhuaweicloud.com/v1/oauth2/tokens` 自动完成轮换。
- 轮换支持 `pdp5_for_agentarts` 与 `codearts-agent` 客户端身份，并保留账号原有的会话与网关信息。
- 没有刷新令牌的账号在到期后提示重新登录，不会在静默失败后反复重试。

## 模型与调用

- 动态目录从上游 MaaS 网关 `GET /v2/models` 读取，并与计费倍率表 `GET /v1/model-token-rates` 自动交集过滤，剔除测试或未开放的失效型号（如 `deepseek-v4.1-flash-1017` 等）。
- 原始模型调用名为 `officeace/cn/<模型ID>`，支持 Chat Completions、Responses、Anthropic Messages 接口。
- 支持全局统一模型绑定（如 `deepseek-v4.1-flash`、`glm-5.3` 等），跨上游自动负载均衡与重试。
- 针对 GLM-5.3 系列上游参数限制，网关自动剥离不被接受的 thinking / stream_options 参数，保证请求顺利执行。

## 积分与每日签到

- **额度查询**：调用 `GET /v1/subscription`（V11-HMAC-SHA256 签名），解析套餐配额与活动奖励配额（`officeace_points`），展示总积分、已用配额、剩余额度与到期时间。
- **每日签到**：支持单账号手动签到与全局定时自动签到，调用 `POST /v1/subscription/bonus/claim`。接口具备幂等性，已领状态正确识别并同步更新本地记录。
