# P4a OpenCode Zen / Go 分批验收

日期：2026-11-16。**源码接入及隔离验收已完成，已注册到应用；真实 Zen/Go 账号验收待完成。**

## 已完成

- API Key 明确区分 zen（按量）和 go（订阅），来源范围独立，同一密钥也不混为同一账号；没有伪造 OAuth 或续期时间。
- 参考固定 Sub2API 的协议分流与窗口字段，保留 LGPL 文本和来源摘要；不迁移数据库、分布式锁、额外缓存或额度刷新任务。
- 官方文档已经迁移到 Console，推荐 /inference 系列端点。采用当前公开端点，按官方元数据修正 Go MiniMax M3.1 Flash 协议例外，保留 Zen/Go 的 Qwen3.8 Max 协议差异。
- 模型参数读取真实公开目录的 limit、modalities、capabilities.tools。只发布匹配已选原生协议的文本型号；Gemini/System One 暂不纳入，Zen 免费模型按官方外部客户端限制排除。
- Go 滚动/周/月额度解析保留未知与 0，不伪造美元总量；Zen 按量余额暂未接入。

## 证据与边界

没有使用 API Key，对官方公开目录执行只读 GET：Zen 86 条、Go 30 条（此次快照）。其中当前三个协议范围内保留 64 / 29 条元数据，**不是已验证可调用的账号模型**。当前及旧版 Go usage 地址无凭据均返回 401。

公开目录返回 HTTP 200 **不能证明密钥有效**。现已提供 Stream 和 Verify：Verify 读取目录后仅执行一次短推理，优先选择目录中的 minimax-m3.1-flash，否则使用首个支持型号；失败不换型号重试。正式开放导入前，界面必须明确说明此操作可能消耗额度。已在 app 注册，并在面板导入前显示此说明；本机正在运行的服务尚未升级。

隔离测试：套餐身份、固定路径、协议分流、上游能力字段、未知值、异常参数、额度零值与重置、公开快照解析、错误 HTTP/重定向不回退。通过 `go test -race ./internal/opencode ./internal/providerhttp`、新包 vet、架构检查和 diff 空白检查。日志 `/tmp/codearts-p4a-foundation.log`。

## 生成与认证验证补齐

- Chat Completions、Responses、Messages 按固定原生路径转发。共用已有转换器；Chat/Messages 提取为无状态 chatwire/messagewire，旧平台保留薄包装，未增加账号、持久化、缓存或重试层。
- 原样传递已有 Request.SessionID 到 x-opencode-session，使用自己的 User-Agent；未提供会话 ID 时不伪造跨请求会话，也不增加第二份会话存储。
- 隔离 HTTP 测试验证三个协议的实际请求体、固定端点、流式结果、连续请求会话头；每次生成仅一次上游调用，没有目录查询。Verify 的公开目录成功但生成 401/403/429 时必须失败。
- 空流、仅 DONE、错误事件、缺少终止事件和取消请求不能报成功。原 Claude、ChatGPT、Grok、cnapi 测试覆盖原有图片、工具及用量行为。
- 定向竞态检查通过，日志 `/tmp/codearts-p4a-stream-race.log`；三协议图片块/工具定义转发测试见 `/tmp/codearts-p4a-native-media.log`。全量后端 `go test ./...` 通过，日志 `/tmp/codearts-p4a-backend.log`；相关包 vet、架构检查和 diff 检查通过。Messages 抽取与原实现逐字归一化比较一致。这不是使用真实密钥的外部验收。

## 应用、面板与分发

- 应用注册第 17 个平台，配置、来源校验、共享名称自动同步均支持 opencode/zen 与 opencode/go；两个模式加入同一共享名称时保留独立来源。
- 面板提供独立套餐选择及遮盖密钥输入，隐藏不适用的浏览器授权；中英文提示短推理的额度消耗，保存失败不关闭对话框，成功清空密钥。
- 24 组网关场景覆盖 Zen/Go、原始/共享名称、三个对外协议和流式/非流式，验证实际来源、输入/输出/缓存计量、并发释放、分组禁止访问。另有账号导入认证失败及落盘失败不报成功的测试。
- Docker、Linux 和 Windows 包加入 OpenCode 的 LGPL 许可与来源清单。
- 浏览器隔离验收 OpenCode、国内官方 API、Gemini CLI 和 Grok 全部通过；报告 `/tmp/codearts-p4a-browser/browser-results.json`。网关及导入竞态测试通过，日志 `/tmp/codearts-p4a-integration-race.log`。

全量 `make check` 通过，日志 `/tmp/codearts-p4a-check.log`；Linux 与 Windows 编译通过，测试产物为 `/tmp/codearts-p4a-panel` 和 `/tmp/codearts-p4a-panel.exe`。未覆盖运行程序。

## 真实验收剩余任务

1. 使用自己的 Zen 按量与 Go 订阅 API Key，分别验证导入、生成、图片、工具往返和用量；隔离测试不替代订阅授权证明。
2. 验证真实 Go 窗口与重置时间；Zen 余额仍明确未实现，不显示伪造余额。
3. 验证真实客户端会话头及缓存表现；没有会话 ID 时不保证上游缓存命中。
4. 升级运行服务、推送或发布仍需按交付流程执行，本批没有执行。

未部署、推送或发布。尚未调用任何真实 OpenCode 模型。

来源：[官方推理接口](https://opencode.ai/docs/console/api/inference/)、[官方 Go](https://opencode.ai/docs/console/go/)、[本地来源清单](../../backend/internal/opencode/UPSTREAM.md)。
