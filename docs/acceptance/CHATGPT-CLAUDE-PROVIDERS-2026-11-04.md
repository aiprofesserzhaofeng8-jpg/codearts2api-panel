# ChatGPT / Claude 原生反代源码验收

日期：2026-11-04。项目：`/home/white/codearts2api-panel`，基线 `a608ede` 的工作树；保留原有未提交的 Qoder / Kiro 接入。本次为源码实现与隔离验收，未重启本机或远端服务，未操作真实 `data/state.json`、`/opt/codearts2api` 或 GitHub Release。

## 实现范围

- `chatgpt`：ChatGPT/Codex OAuth PKCE、Codex auth.json 导入、OpenAI API Key、令牌续期、动态目录、原生 Responses 转发。
- `claude`：Claude OAuth PKCE、稳定 UUID 凭据导入、Anthropic API Key、续期、有界目录分页、原生 Messages 转发。
- 对外仍使用 Chat Completions / Responses / Messages；没有新增推理认证入口，普通 API Key 不能管理账号或提交上游凭据。
- 共享已有账号池、并发租约、分组权限、原始模型白名单、统一模型、预算与计量、凭据互斥/CAS 保存。没有新增账号调度或推理重试层。
- 平台设置继承公共提示词，支持独立覆盖；地区固定 `global`。账号/用量筛选及中英文授权提示已接入。
- Claude 原生思考、签名、redacted thinking、大整数工具参数和连续工具结果；带原生思考的统一名称限定到当前已授权的 Claude 来源。
- 流中断、HTTP 200 流内错误、未闭合工具、缺失最终用量不当成成功。可见 SSE 输出后不换号重放；非流式私有缓冲失败沿用既有总重试预算。
- Messages 响应分别报告普通输入、缓存读取与缓存写入；现有 USD 定价仍没有独立缓存写入费率。

## 验证

环境为 Linux amd64，固定 Go 1.27.3 工具链和仓库锁定前端依赖，使用 `GOMAXPROCS=2 GOFLAGS=-p=2` 限制构建负载。所有账号、密钥、token 和上游均为 fixture，服务使用临时状态和回环随机端口。

| 检查 | 结果 |
| --- | --- |
| `make check GO=<固定工具链>` | 通过；包含完整 Go 测试/vet、前端 37 个测试、API 类型同步、架构边界、86 项部署测试及状态迁移检查 |
| 主推理矩阵 | 48 组通过：2 平台 × 2 认证方式 × 3 外部接口 × 2 流式选项 × 文本/工具输出 |
| OAuth 预览/保存 | 两平台通过：PKCE/state、错误回调拒绝、只兑换一次、检查不落盘、显式确认、API Key 不能获得管理权限 |
| 续期 | 两平台通过：先提交新凭据再推理，稳定身份不变，落盘后重开仍为新 refresh token |
| 统一模型 | 通过：公开名称菜单、两个平台切换、实际来源日志、原始型号白名单拒绝及原生思考候选限制 |
| Claude 签名工具对话 | 通过：Messages 流式/非流式输出及实际返回内容的下一轮工具结果输入；`9007199254740993` 不舍入 |
| 取消与错误 | 通过：取消关闭上游读取，截断返回失败，使用量与租约不误记成功，错误响应不回显 token |
| 浏览器回归 | 19 场景通过，包括新增中/英 OAuth 回调、预览确认、API Key 导入、提示词继承覆盖、账号/用量筛选及 390px 手机布局 |
| 定向 race | 通过：chatgpt、claude、providerhttp、protocol 和 server 的相关用例；包含最终用量缺失、签名连续对话和流中断回归 |

本地检查日志：`/tmp/codearts-subscriptions-check.log`、`/tmp/codearts-subscriptions-race.log`。最终可执行测试程序：`artifacts/subscriptions-panel-test`（未覆盖现有安装程序）。浏览器结果与手机截图位于 `artifacts/subscriptions-browser/`、`artifacts/subscriptions-zh-CN-mobile.png` 和 `artifacts/subscriptions-en-US-mobile.png`；这些隔离产物不入 Git。

## 未验证与边界

未使用真实 ChatGPT 或 Claude 账号兑换、刷新、目录查询或推理。订阅权益、OAuth 对第三方客户端的限制、真实模型可用性、套餐限流与实际账单尚未验收；不能以模拟 HTTP 200 或目录成功代替真实推理验收。

没有导入浏览器 Cookie、执行本地 CLI/工具、伪装官方 CLI 身份、启用独立搜索/奖励或实现订阅余额查询。Codex 输出限制、托管工具、跨协议元数据和 Claude 缓存写入定价等限制见 [ChatGPT](../CHATGPT.md)、[Claude](../CLAUDE.md)。

协议参考固定在 Sub2API `4a4a83ee5ec86b19d3a2dd9b6471de5128963bec`，同时核对 OpenAI / Anthropic 官方协议文档。新增适配器为独立实现；项目原有 responsescompat 复用代码仍保留其 LGPL 声明。
