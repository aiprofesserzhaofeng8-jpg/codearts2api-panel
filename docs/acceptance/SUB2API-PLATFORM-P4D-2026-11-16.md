# P4d TypeSafe / System One 分批验收

日期：2026-11-16。状态：原生客户端、公开决策入口、应用与导入面板已接通；消费者准入与结算隔离验收通过；独立原生目录及真实验证未完成。

## 已确认的协议区别

TypeSafe Jev 返回带概率的结构化决策，不是生成聊天文本的 LLM。官方接口为 POST /v1/systemone，输入 model/state/questions；不是 Chat Completions、Responses 或 Messages。不应出现在普通聊天客户端的可用模型清单中。

当前官方 OpenAPI 的 GET /v1/models 需要 Bearer，响应为 models/name/description/release_date；允许返回的模型名或别名。固定 Sub2API 原校验器只允许 jev-latest，本批按当前 schema 放宽，不限制为单个旧别名。

## 实现

- 复制并适配 Sub2API systemone.go 校验逻辑，复用端点、单次原生请求和有界原始响应设计；保留 LGPL 文件、源文件与本地摘要。
- API Key、固定 global 范围、独立身份摘要，拒绝混合凭据/自定义端点。Verify 只读取受认证的模型目录，不发送付费决策。
- DecisionModels/Decide 是明确的原生操作，不伪装成 provider.Adapter.Stream。
- Noul、Choice、Score 支持结构化 state/instructions/criteria。Score 按官方 schema 允许 1～10 层。重复字段、大小写歧义、非法问题类型及 stream=true 在请求发出前拒绝。
- 请求/响应各 4 MiB 上限，取消传播、拒绝重定向，不泄露上游回显。原始 JSON 保留大整数与扩展答案字段。
- 答案缺失、类型不匹配、错误对象和损坏 JSON 不报成功。合法答案伴随缺失/非法用量时保留答案，但 Usage.Known=false；不把未知记为零消费。
- 初批不支持 assets/Box、multipart、长上下文显式 opt-in。明确报不支持，不静默裁剪或擅自开启实验选项。

## 验证与边界

定向竞态测试通过：`/tmp/codearts-p4d-foundation.log`。覆盖三个问题类型、版本型号、结构化 state、重复键/别名冲突、无效输入不发请求、固定端点与 Bearer、超限响应、用量未知、取消与重定向。

全量 `make check` 通过（`/tmp/codearts-p4d-check.log`），包括架构、前后端与分发回归检查。

官方 schema 经浏览工具读取成功；本机直接 HTTPS 读取失败于 TLS 握手。未使用真实 API Key，没有真实决策或账号授权验证。测试用例为文档字段构造，不是实际上游回执。

## 原生入口接入

- provider.Adapter 的 Stream 拆为可选 Streaming 能力；TypeSafe 实现 Decision，其他聊天平台保留原 Streaming 方法。没有伪造 Chat 响应或将 Jev 加入聊天模型清单。
- POST /v1/systemone 使用原始 model/state/questions，保留重复键校验，原生来源为 typesafe/global/<model>。不应用聊天别名/默认模型或自动换源。
- 复用 withAuth/withRateLimit/withKeyBudget、同一 Admission 原子选账号/租约、ProviderCredentials 及用量账本。原生权限检查复用 AccessScope 和账号/型号允许范围，不依赖聊天目录。
- 一次请求仅一次上游决策；状态错误、未知计量、取消不触发重复收费请求。受认证型号权限仍由上游最终判断。
- 已测允许/空分组、无效 API Key、原生模型与响应保留、已知/未知 token、HTTP 401/429/500 错误脱敏、非法重复字段不发请求、取消释放并发。定向竞态通过：/tmp/codearts-p4d-gateway-final.log。
- 沿用网关错误映射：上游 401 为 502 credentials_error，上游 500 为 502 upstream_error；不是把上游凭据错误误报为用户面板密钥错误。
- 面板 TypeSafe 导入验证不发付费请求，提供独立接口提示。TypeSafe/Cline/Command Code 浏览器隔离用例通过：/tmp/codearts-p4d-browser/browser-results.json。
- 导入身份失败和落盘失败均不返回成功，重试保存通过；定向竞态日志 /tmp/codearts-p4d-save.log。
- TypeSafe LGPL 文件加入 Linux/Windows/Docker 分发。

全量 `make check` 最终通过：`/tmp/codearts-p4d-integration-final.log`。早期测试将上游 401/500 错误期待为直接透传，已按既有网关 502 映射修正测试；取消用例修正模拟服务未消费请求体导致的清理等待，最终竞态用例通过。Linux/Windows 编译通过，产物 `/tmp/codearts-p4d-panel`、`/tmp/codearts-p4d-panel.exe`，未安装。

## 剩余任务

1. 原生模型目录的独立展示及刷新、型号白名单配置便利性；普通聊天模型列表继续排除 TypeSafe。
2. 消费者准入与结算已补齐隔离验收（见下）；真实计费与价格仍需真实账号验证。
3. 真实账号验证和当前本机 TLS 握手故障定位；未运行真实付费决策。
4. 图片、Box、multipart、长上下文 opt-in 不在已实现范围。

未部署、未推送或发布；未新增存储、后台任务或外部文件缓存。

来源：[TypeSafe 介绍](https://docs.typesafe.ai/introduction)、[官方 OpenAPI](https://api.typesafe.ai/openapi.json)、[来源清单](../../backend/internal/typesafe/UPSTREAM.md)。

## 消费者准入与结算补充验收

2026-11-16：新增 systemone_consumer_test.go，并将取消用例扩展为启用钱包的普通用户路径。此次只增加测试和验收记录，没有修改生产准入/账本逻辑。

- 已知用量进入钱包扣费和请求预算；释放额度预留，余额耗尽后返回 402 且不调用上游。
- 未知用量保留合法答案，余额不假扣，标记 ReviewRequired；后续调用返回 402，不把未知消费记作免费。
- 两把密钥共享同一用户并发和请求预算：占位时返回 429，释放后可调用，预算耗尽后返回 402。
- 原生完整型号白名单拒绝未授权型号（403），允许列表中型号正常执行。
- 已授权但缺少参考价格的型号，在启用钱包时返回 402，不发送付费决策。
- 取消发生前确认已有用户在途槽和额度预留；取消后账号槽、用户槽和额度预留全部归零。

定向竞态：`go test -race ./internal/server -run TestSystemOne -count=1` 通过，日志 `/tmp/codearts-p4d-consumer-check.log`。模拟上游；没有使用真实凭据、部署或推送。此次测试变更未重复全量构建，前一批全量记录保持其原验收范围。
