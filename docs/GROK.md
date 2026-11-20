# Grok Build 接入

2026-11-16：源码与隔离验收完成；真实 xAI 账号验收待完成。尚未部署或发布。

## 添加账号

「添加账号」→ Grok Build → 浏览器授权 → 生成链接。在 xAI 完成授权后，复制完整 `http://127.0.0.1:56121/callback?code=…&state=…` 地址粘贴回面板，检查预览后保存。回调本机没有监听器时页面可能显示连接失败，仍可复制地址栏。

授权码只兑换一次，目录校验失败可重试检查。上游必须接受访问令牌并返回可用目录后才能显示已验证预览。保存失败不会显示成功；重试保存不重复兑换授权码。

也可导入完整 Build OAuth JSON：`access_token`、可选 `refresh_token`、`expires_at`（Unix 秒）、`scope: "build"`、可选 `auth_mode: "oauth"`。账号与团队取自 access_token 元数据，上游负责验证访问令牌。普通 xAI API Key、网页登录 SSO/Cookie 和账号密码不属于本次接入。

## 模型与生成

来源 ID 为 `grok/build/<上游完整型号>`。固定使用 `https://cli-chat-proxy.grok.com/v1`，目录 `/models`、生成 `/responses`。按 Sub2API 的固定 CLI 身份头发送，不自动换端点、版本、型号或偷偷调用其他模型描述图片。

目录刷新沿用现有共享名称自动同步；例如 `grok-4.7` 对应 `grok/build/grok-4.7`。已有权限与停用选择保留。未知图片/工具/上下文参数不通过猜测补齐，目录成功不等于所有模型能力均已实测。

外部 Chat Completions、Messages、Responses 均接入现有网关转换；支持文本、用户图片、函数工具及多轮文本工具结果。图片地址直接作为原生输入传给上游，网关不下载保存。上游能否接受具体图片由具体型号决定。

不支持音视频、生成媒体、工具结果中的图片、自定义内置服务端工具或加密推理内容重放；不静默替换成其他模型。未知请求能力不能用模拟测试替代实测。原始型号是否接受 reasoning_effort 等参数由上游决定，不猜测兼容值。

EOF、空响应、业务错误不当作成功。显式长度截断保留 length；缓存与 reasoning_tokens 保留，不重复加到输出总量。客户端取消及时退出，复用现有租约和账本。

## 订阅窗口

读取 `/billing?format=credits` 的 creditUsagePercent/currentPeriod.end，及 `/billing` 的 monthlyLimit/used/billingPeriodEnd。

- 周窗口按上游百分比显示；月额度原始为美分，转换为美元显示。
- 两个窗口的重置时间独立，不把月日期当周日期。
- 缺字段为未知，明确的 0 保留；不猜套餐、不生成本地软配额。
- 不累加预付费、按量付费和产品明细；这些字段本次未展示。
- 任一窗口接口失败则报告查询失败，不把半份结果标为完整成功。

查询只在现有额度刷新流程发生，不增加每次推理的目录/余额查询或第二份持久化额度表。

## 验收边界

本地假上游已覆盖 OAuth、刷新、目录、生成、图片/工具、错误流、用量、保存失败、并发续期、三个 API 协议、权限及浏览器流程。未使用真实 xAI 账号；未验证真实套餐权限、完整多轮工具、图片及自然续期。Linux/Windows 构建不等于已部署，Windows 未实际运行、Docker 未构建运行。

[验收记录](acceptance/SUB2API-PLATFORM-P3B-2026-11-16.md) · [来源与许可](../backend/internal/upstream/grok/UPSTREAM.md)
