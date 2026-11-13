# R3：Messages 工具历史兼容验收

日期：2026-11-07。结论：R3 源码修复及计划要求的隔离、真实上游、代表性 Pi 验收完成；尚未部署或发布。R1、R2 和型号身份第一批的已有本地改动保留。

## 根因与修复

原来的 RequiresNativeMessages 将任何 thinking 内容块都判定为必须调用原生 Claude。网关把 Chat reasoning_content 转成 Messages thinking 返回后，客户端将它连同 tool_use 和 tool_result 回传，共享模型在第二轮被错误拒绝。另一方面，非原生 MessageToOpenAI 没有保留 thinking，直接来源路径可能静默丢失普通推理历史。

生产修改集中在 [anthropic.go](../../backend/internal/protocol/anthropic.go)：

- 无签名、空字符串签名、null 签名的助手 thinking 映射到 reasoning_content。多个普通块按顺序连接，文本、工具调用和结果继续使用各自字段；仅有思考的助手消息也保留。
- 非空签名、异常签名类型及 redacted_thinking 不能当作普通推理文本。共享入口继续选择兼容原生 Messages 来源；直接非原生转换也明确拒绝，不再丢掉块后继续请求。
- 保留原生块及签名透传路径。不生成假签名，不将另一模型的普通推理认证为 Claude 原生签名。
- thinking enabled/adaptive、budget_tokens、output_config 的现有原生要求和拒绝边界保持。没有新增模型名单、后台请求、存储、会话缓存或路由器。

这是普通历史和原生历史的区分，不是对任意历史来源的密码学身份认证；原生供应商仍负责签名有效性及具体型号兼容性。ChatGPT、Trae 等其他适配器的全部扩展历史格式不在本批扩大支持范围。

## 隔离验证

[协议测试](../../backend/internal/protocol/anthropic_history_test.go)覆盖普通/空签名/null 签名、签名/加密块拒绝普通转换、异常类型、用户角色拒绝、显式控制参数、仅思考消息、原生签名保留。混合用例验证两个工具调用及结果归属、9007199254740993 精度、思考文本、可见文本与工具图片均保留。

[HTTP 测试](../../backend/internal/server/anthropic_history_test.go)通过真实处理器和模拟 WorkBuddy HTTP 上游完成首轮推理加工具调用、第二轮结果消费，分别覆盖流式、非流式及合法拒答；使用实际返回的内容块构造历史，并检查租约释放。共享和直接非原生来源对签名/加密块均在上游调用前拒绝。既有 Claude 原生签名、用量、工具历史测试继续通过。

- 完整 make check 通过：前端 56 项测试、Go 全包测试和 vet、Linux vet、控制台、架构及 100 项部署维护测试等。
- protocol、claude 完整包测试通过；protocol/server 的新增历史测试和既有 Claude 签名/响应字段测试专项 race 通过。
- 日志：/tmp/r3-protocol.log、/tmp/r3-race.log、/tmp/r3-check.log。

## 真实平台逐项验收

使用临时隔离网关加载当前源码，每个公开名称固定到一个待验收来源，随后通过本机生产网关的原始 Chat 入口访问已有账号。这样验证新代码的共享目录、Messages 转换、响应转换及真实上游闭环，同时避免替换线上程序或改写生产模型配置。该方式不覆盖共享模型其他候选来源的自动故障切换。

每项均为：真实首轮工具调用 → 将返回 thinking/tool_use 原样放回历史 → 客户端提供随机 CHECK 常量 → 最终回答准确包含该常量、结束状态合法且不再调用工具。未执行模型生成的任意命令。测试输出预算 2048、单请求约 55 秒；没有为取得通过结果重测失败项。

| 公开名称 | 固定来源 | 非流式 | 流式 | 原始来源非流式 |
| --- | --- | --- | --- | --- |
| workbuddy-hy3 | workbuddy/cn/hy3 | PASS | PASS | PASS |
| workbuddy-hy3-x | workbuddy/cn/hy3-x | PASS | PASS | PASS |
| workbuddy-hy4free | workbuddy/cn/hy4free | PASS | PASS | PASS |
| workbuddy-hy4std | workbuddy/cn/hy4std | PASS | PASS | PASS |
| kimi-k2.8-preview | qoder/cn/kmodel | PASS | PASS | PASS |
| kimi-k3-1 | workbuddy/cn/kimi-k3-1 | PASS | PASS | PASS |
| kimi-k3.1-pro | qoder/cn/kmodel_latest | PASS | PASS | PASS |

共享 14/14、原始来源 7/7 全部通过；21 项首轮均实际包含 thinking，21 个第二轮转发均同时包含 reasoning_content 与工具结果。工具参数及历史未为绕过错误而删减。没有上游受阻项。此次 Qoder 两个型号通过不代表 R4 的所有受影响型号已解决。

Pi 使用临时 agent 目录和 anthropic-messages provider、公开名称 workbuddy-hy3，完成 read 工具读取合成临时文件并返回随机内容，1/1 通过。仅启用 read，关闭外部扩展、MCP 和上下文文件，临时配置随测试清理；未改写 ~/.pi/agent/models.json。Pi 的自身历史转换行为与直接 HTTP 原样回放分别验收，不能用 Pi 成功替代上述 21 项历史保留检查。

真实调用共 44 次转发：共享 28 次、原始来源 14 次、Pi 2 次。这些调用经过原有计费/健康路径，会正常产生用量和日志；测试未关闭生产准入或绕过分组权限。

本机忽略目录 artifacts/r3-messages-history-2026-11-07/ 保存 targets.json、results.json、summary.json、raw-results.json、raw-summary.json、pi-results.json、forwarded.jsonl 及复验脚本。forwarded.jsonl 仅记录来源、思考消息数量及工具结果数量，不记录提示词、凭据或工具正文。历史失败基线 artifacts/model-capability-audit/public/results.json 保留，未覆盖。

## 交付与部署边界

备份：/home/white/backups/codearts2api/before-r3-20261107-174146/source-changes.tar.gz，含修改前未提交源码改动，另存 Git HEAD；不含生产账号状态。

本机服务 codearts2api.service 仍运行原 PID 2102403，启动于 2026-11-06 22:29:46 CST。本批未升级、重启生产程序、推送 GitHub 或发布 Release。R3 的“完成”指源码及验收完成；线上生效需后续正常构建升级。原生附件协议、R4 工具异常、R5 健康展示、R6 客户端参数继续按原计划处理。

同日按用户要求直接复用 Sub2API 消息转换核心后，重新执行了验收，见 [Sub2API 消息转换复用](SUB2API-MESSAGES-REUSE-2026-11-07.md)。本文保留原 R3 修复和首次验证记录。
