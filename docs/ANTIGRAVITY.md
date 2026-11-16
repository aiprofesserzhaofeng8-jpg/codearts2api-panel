# Antigravity 接入

2026-11-16：源码、隔离网关与浏览器验收通过；真实 Google 账号验证待完成。尚未部署或发布。

## 添加账号

1. 准备本人可使用 Antigravity 的 Google 账号。已有项目直接使用；未返回项目时，在本次浏览器授权中按上游默认套餐初始化。
2. 面板「添加账号」选择 Antigravity → 浏览器授权 → 生成链接。
3. 在浏览器完成 Google 授权，复制地址栏完整 `http://localhost:8085/callback?code=…&state=…` 地址粘贴回面板。该地址没有本机监听器时，页面连接失败不影响复制回调。
4. 检查账号预览并确认保存。保存失败会显示错误，可在原会话内重试。
5. 成功刷新目录后按既有规则生成或追加共享模型来源，不扩大已有分组权限。

也可导入完整 JSON：`access_token`、可选 `refresh_token`、`subject`、`project_id`、`expires_at`（Unix 秒）、`scope: "global"`、可选 `auth_mode: "oauth"`。不要使用 Gemini CLI 的凭据替代，OAuth 客户端不同。

浏览器授权缺少项目时，调用 onboardUser 初始化，仅使用上游明确提供的默认套餐；没有默认套餐时提示在官方客户端处理，不编造 project_id 或猜测其他套餐。仅 done=false 时最多检查 5 次、间隔 2 秒；网络/HTTP 错误即停止，可稍后重新检查授权。导入校验、续期和推理均不会执行初始化。没有 refresh_token 的账号到期需重新授权。新账号初始化已通过隔离测试，真实 Google 账号仍待验证。

## 路由、目录和额度

来源为 `antigravity/global/<上游完整型号>`。目录和推理固定使用 `daily-cloudcode-pa.googleapis.com`，不在后台切到另一个端点或另一个型号。

目录来自 `fetchAvailableModels`；上下文、输出上限和图片能力只读取上游实际字段。未提供的能力保留未知，不能把目录成功当作生成验收。特殊图片/音频生成型号不通过此文本输出接口公布。

配额按型号显示上游剩余比例与重置时间：缺值为未知，0 为耗尽。不把共享窗口累加成余额，不推算美元总额。

## 已实现与边界

- Chat Completions、Responses、Messages 的流式与非流式生成。
- 内嵌 base64 图片、函数工具及工具结果；工具签名通过 `extra_content.google.thought_signature` 往返。远程图片、音视频及生成媒体输出明确报错，不下载保存文件。
- 不注入假签名，不改写系统身份，不将搜索或图片请求自动切到其他模型。客户端仍须保留签名；已知本机 Pi 1.4.0 丢弃该扩展字段的限制没有在此次修改客户端。
- reasoning_effort 转为 Antigravity 数值预算；Claude 显式输出上限优先，不私自扩大。小于等于 1024 的输出限制与启用 Claude 推理不兼容时返回具体错误。
- SSE 错误、空响应和未结束断流不会返回成功；用量保留缓存与推理计数，缺失不当作零。
- 继续使用现有凭据协调器、状态提交、选号、租约和账本。无新数据库、签名缓存、文件存储或每次生成的目录/配额查询。

真实 Google 登录、账号权限、不同型号、图片与工具、自然续期尚未实测。上述支持范围由隔离测试证明实现行为，不代表已证明所有上游型号和客户端组合可用。

[验收记录](acceptance/SUB2API-PLATFORM-P3A-2026-11-15.md) · [复用来源](../backend/internal/antigravity/UPSTREAM.md)

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
