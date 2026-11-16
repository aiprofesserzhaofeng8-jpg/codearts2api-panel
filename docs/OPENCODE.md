# OpenCode Zen / Go

本项目支持 OpenCode 的 Zen 按量 API Key 与 Go 订阅 API Key。两者作为同一平台的两个独立来源范围，不共享账号身份或额度。

## 添加账号

在账号管理选择「添加账号 → OpenCode Zen / Go → 导入」，选择 Zen 或 Go，填写自己的 API Key 后点击「校验并导入」。无需浏览器 OAuth。校验会读取公开目录，再执行一次短推理，可能消耗额度；上游拒绝、响应不完整或本地保存失败均不会提示成功。

校验只证明所选范围的一次推理可用，不代表目录中的每个型号都有权限。未提供真实 Zen/Go 凭据前，本项目只能报告隔离验收通过。

## 模型与协议

来源 ID 为 `opencode/zen/<型号>` 或 `opencode/go/<型号>`。目录刷新沿用现有模型同步；明确的同名型号加入现有共享名称，保留原来的停用及分组权限。Zen 与 Go 不混为同一来源。

按型号原生协议使用 Chat Completions、Responses 或 Messages，复用已有图片、工具和用量转换器。当前不发布需要 Gemini 或 System One 协议的型号。Zen 免费型号按官方外部客户端限制过滤；不伪装为官方客户端绕过限制。

使用固定的 `/inference` 和 `/inference/go` 地址，不执行目录返回的自定义 URL。普通生成不会额外查询目录或额度，已有会话 ID 透传给 `x-opencode-session`，没有新增会话缓存或调度器。

## 额度和限制

Go 显示上游滚动、周、月窗口及重置时间；未返回的值显示未知，不能当成零。Zen 余额暂未接入。API Key 没有虚构的到期时间和自动续期。

第三方客户端能否调用、套餐是否允许及真实图片/工具表现最终以上游结果为准。真实账号验收尚未完成，不能将公开目录 HTTP 200 当作密钥认证成功。

来源及许可见 [OpenCode 来源清单](../backend/internal/opencode/UPSTREAM.md)。官方文档：[推理接口](https://opencode.ai/docs/console/api/inference/)、[Go 套餐](https://opencode.ai/docs/console/go/)。
