# P4c Cline 分批验收

日期：2026-11-16。状态：应用/面板基础接入及隔离测试完成；跨协议透明扩展隔离验收通过；真实账号及 Messages 客户端验收未完成。

## 基础实现

- 参考固定 Sub2API 的 Cline 端点、账号信封、微积分余额与套餐窗口，保留 LGPL 来源、许可及文件摘要。
- 首批仅 API Key，固定 global 范围，不伪造 OAuth/刷新令牌或到期时间。同一密钥的积分、ClinePass、免费模型不拆成互相伪装的账号。
- 所有原始型号（含 anthropic/*、openai/*、cline-pass/*、cline-free/*）均原样发往 Chat Completions，不按模型厂商错误切到 Messages/Responses 原生端点。复用无状态 Chat 转换器。
- Verify 使用受认证 users/me，不依赖公开目录或免费推理，不产生额外推理消费；这只证明账号认证，不保证模型有权限。
- 目录未知能力/窗口参数保持未知，重复 ID 去重，非法 ID 拒绝。
- users/me 后查询对应用户 balance；检查可选 userId 一致。微积分以精确十进制除以 1e6，保留六位，不经过 float 舍入。
- ClinePass 的五小时/七天/三十天滚动窗口独立显示；百分比为零与未知分开，没有 reset 时不虚构时间；usage-limits 404 保留余额，其他失败不假装没有订阅。
- 没有新增缓存、数据库、轮询、自动付费切换或账号级限流。

## 证据

本机无凭据 GET /api/v1/models 返回 HTTP 200，共 453 行；无凭据 users/me 返回 HTTP 401。目录脱敏快照只保留 ID 并记录摘要，不是账号可调用清单。

隔离测试：认证失败与公开目录分离、信封错误、缺少身份、厂商/钱包前缀保留、图片/工具定义转发、微积分大数精度、无订阅与 401 区分、未知/零窗口、空流、截断与取消。`go test -race ./internal/cline ./internal/chatwire`、vet、架构检查和 diff 检查通过；日志 `/tmp/codearts-p4c-foundation.log`。

## 本批接入

- 注册第 19 个平台、API Key 导入入口，明确校验不消耗推理额度。中英文提示，失败保留密钥供重试，成功清空。
- HTTP 与 SSE 结构化限额映射到既有模型级故障机制；按量、订阅、免费模型错误不禁用整个账号。与 Sub2API 的钱包级冷却不同，仅实际失败型号冷却，不引入第二套状态。401 仍是账号凭据错误。
- 不读取 metadata 中可能回显的请求文本作限额判断；只返回固定脱敏错误，不虚构重置时间。
- 12 组原始/共享名称、三种客户端协议、流式/非流式的基础生成/计量/缓存/ACL/并发释放测试通过，严格保存失败与重试及模型冷却无账号惩罚测试通过。`/tmp/codearts-p4c-integration.log`。
- plain reasoning 映射到 reasoning_content；Chat 流保留 reasoning_details。测试覆盖加密块不作为普通文本拼接，不把仅推理帧误判为空流。
- 对 Cline 和其他 Chat 转换使用方的定向竞态检查通过：`/tmp/codearts-p4c-wire-race.log`。
- 发行包与 Docker 已加入 Cline 来源清单与 LGPL 许可。

全量 `make check` 通过（`/tmp/codearts-p4c-check.log`）；随后 reasoning 字段适配的定向竞态测试、vet 通过。Cline、Command Code、OpenCode、额度页面浏览器测试通过（`/tmp/codearts-p4c-browser/browser-results.json`）。Linux/Windows 编译通过，产物为 `/tmp/codearts-p4c-panel`、`/tmp/codearts-p4c-panel.exe`；未覆盖运行程序。

## Chat 加密推理往返补齐

- 非流式 Chat 聚合保留 reasoning_details 的原始块及出现顺序，按候选回复隔离，不转成推理文本，不重写加密内容或厂商扩展字段，不经 float 解码大整数。
- 隔离网关两轮测试：Cline 返回 reasoning_details + 工具调用 → 聚合为 assistant message → 客户端带原 assistant message 和工具结果再次请求 → 模拟上游确认元数据、工具 ID、模型来源未变化。测试同时检查请求数和并发释放。
- Cline 官方文档声明 finish_reason=error 表示失败。现在明确拒绝，不能仅因收到 DONE 就报成功。
- 定向竞态测试通过（`/tmp/codearts-p4c-reasoning.log`）；协议与九个相关包回归通过（`/tmp/codearts-p4c-reasoning-regression.log`）。这是模拟上游的协议证明，非真实加密签名有效性证明。
- 本批全量 `make check` 通过（`/tmp/codearts-p4c-reasoning-check.log`）；补充部分输出后错误结束测试通过。没有新增本地存储、缓存或调度状态。
- 加密详情当前按收到的块顺序保留；没有证据支持将多个同 index 块拼接成另一种格式，因此不进行这种重写。

## Responses / Messages 透明扩展往返

- ChatMessage/ChatDelta/Responses reasoning item 保留可选 reasoning_details 原始数组；Responses output_item.done 与最终输出保留，回放至 assistant/tool 历史。新 user turn 清除上一轮继承详情。
- Messages thinking block 使用同名 reasoning_details 扩展字段；流式以独立 thinking block 携带，非流式保留，回放时恢复到 Chat assistant 消息。
- 这是显式网关扩展，不是 OpenAI encrypted_content 或 Anthropic signature；不伪造这些字段，不解密、不额外持久化，也不创建按会话缓存。
- 两轮模拟网关 HTTP 测试已覆盖 Chat、Responses、Messages：上游返回加密块及工具调用，客户端回传工具结果后，上游接收到同一加密数据、大整数元数据及工具 ID。单独覆盖流式终止 item/块与新用户轮次隔离。
- 定向竞态通过：`/tmp/codearts-p4c-cross-reasoning.log`。本批全量 `make check` 通过（`/tmp/codearts-p4c-cross-check.log`），未替换运行程序。
- 本机已安装 Pi 的 Responses 模块实际执行离线 stream→message→request 转换，确认完整 reasoning item 的扩展字段能回传。脚本 `/tmp/codearts-p4c-pi-reasoning.mjs`；未联网、未读密钥、未调用真实模型。结论仅针对本次安装版本和 Responses 模块，不代表所有 SDK。

## 剩余

1. 真实 API Key 验证各类模型的工具往返、图片、积分与 Pass 窗口；透明传递不等于上游会接受签名。
2. 实际 Messages 客户端能否保留 reasoning_details 扩展字段尚未验证；丢弃未知字段的客户端不支持该往返，不能标记为完全兼容。
3. 客户端使用真实型号的共享名称验收；未自动去掉 Pass/免费前缀合并，避免混淆身份和计费来源。

本批未部署、未推送或发布。没有读取生产状态或调用真实付费推理。

来源：[官方 API](https://docs.cline.bot/api/overview)、[认证](https://docs.cline.bot/api/authentication)、[模型](https://docs.cline.bot/api/models)、[本地来源清单](../../backend/internal/cline/UPSTREAM.md)。
