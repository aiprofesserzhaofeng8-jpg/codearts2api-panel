# TeleAgent 接入

`teleagent` 对应中国电信 TeleAI 的 TeleAgent，地区为 `cn`。账号池新增「TeleAgent → 凭据导入」，保存前只读查询官方身份。模型请求走 TeleAI 网关，复用现有账号调度、权限、预算、用量账本和协议转换。

## 添加账号

先在官方客户端登录，通过自己获准使用的导出或配置取得完整凭据。在面板中选择「添加账号 → TeleAgent → 下一步」，将凭据 JSON 填入导入框，校验通过后保存。

```json
{
  "token": "<本人账号令牌，可带 tenant: 前缀>",
  "api_key": "sk-<本人获准使用的网关密钥>",
  "device_id": "<原客户端 deviceId>",
  "install_id": "<原客户端 installId>",
  "app_version": "<原客户端版本，例如 2.7.1>"
}
```

`token` 是账号令牌，发到 `X-Token`；`api_key` 是模型网关认证，发到 `Authorization`。二者用途不同。程序不内置第三方共享网关密钥，不会生成或轮换设备信息，不读取桌面进程、浏览器 Cookie 或系统钥匙串。

可使用 `access_token` / `accessToken`、`apiKey`、`deviceId`、`installId`、`appVersion` 字段别名，也支持包在 `auth` 对象中。`account_id` 和 `expires_at` 优先从 JWT 声明补齐；显式过期时间不能延长 JWT 的 `exp`。身份最终通过官方 `GET /superCowork/sapi/cowork/user_info` 复核，JWT 解析本身不等于签名校验。

当前没有提供面板浏览器授权或已确认的自动续期协议。账号不标成长期有效或可续期；到期需重新登录并显式覆盖原账号凭据。现有凭据协调器保留到期前 5 分钟的安全余量，进入该余量后也会要求更新凭据。

## 调用与模型目录

对外仍使用已有三个入口：

- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/messages`

原始型号为 `teleagent/cn/chat-pro`、`teleagent/cn/chat-flagship`、`teleagent/cn/chat-flash`、`teleagent/cn/chat-lite`、`teleagent/cn/chat-nano`。它们来自固定参考版本的客户端预置目录，不代表已验证账号对每个型号的调用权限；没有可确认的动态目录端点，未宣称最大上下文、图片、底层厂商或实际模型版本。

例：调用 Chat Completions 时指定 `"model":"teleagent/cn/chat-pro"`。也可以在账号分组中将它映射到带版本的公开名字，映射与原始型号仍受同一个分组权限入口约束。不会自动把 TeleAgent 的产品档位映射成某个厂商型号。

TeleAI 模型网关使用两阶段 HMAC-SHA256，签名绑定原账号令牌、安装标识、实际发送的请求路径/字节、秒级时间、nonce 和原客户端版本。用户消息、system/developer、工具参数和工具结果经现有策略后转发；服务不执行模型返回的工具。非流式客户端复用现有 SSE 聚合；缺失结束标记、错误帧或断流不伪装成成功。

## 积分与限制

默认查询 `GET /superCowork/sapi/user/portal/companyStore/userCenter` 的 `current_points_balance`。如果用户另行提供同一账号的 `operation_token`（可选 `oper_token`），改用独立积分门户 `GET /meter/super-agent/user/portal/points/detail` 的 `pointsBalance`；门户 Cookie 不发送到推理接口。

返回余额可为数值或十进制字符串，明确的 0 有效。只有余额时只展示余额，不编造已用量、总额或周期。未返回余额显示未知，业务失败显示错误；查询沿用原有缓存/期限/并发控制，不更新推理健康或账本。积分与本地 USD 参考费用互相独立。

每日签到、成长任务、独立搜索和生图 API 尚未接入。导入身份成功不能证明网关密钥、全部模型权限或每种扩展参数已经通过真实调用。未使用真实账号验证模型生成、积分或自然到期；本批自动测试全部使用模拟上游及隔离状态。

## 维护与来源

平台 HTTP/签名/凭据在 `backend/internal/teleagent/`；装配在 app，中立业务不依赖平台包。新设置为兼容字段，状态 schema 仍为 10；旧程序无法使用新平台，回退前需检查账号/分组配置的兼容性，不能盲目恢复旧凭据。

协议依据：[teleagent-proxy](https://github.com/liao96312/teleagent-proxy/tree/875a2b4080ded5f9eebada6d43c94c77ebddc5a5)、[dsh-connect-teleagent](https://github.com/anbulang/dsh-connect-teleagent/tree/14bb4bb16d6961addff63ced79866f9a74391108)、[dsh-codearts-auth](https://github.com/anye-99/dsh-codearts-auth/tree/686658793c982cd0e1baedbcfa10a396ce5d1a09)。三者协议记录存在版本差异，采用可交叉核对的网关签名事实，不采用伪造本地签名密钥的未验证假设。MIT 许可见根目录 `LICENSE.teleagent`，随 Linux、Windows 与镜像分发。
