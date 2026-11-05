# 协议与故障恢复审查（2026-11-05）

## 结论与范围

对照 Sub2API 固定版本 `4a4a83ee5ec86b19d3a2dd9b6471de5128963bec`，检查本项目 Messages/Chat/Responses 转换、八个平台适配入口、目录能力、连接错误分类及既有取消/租约回归。本轮复现 **7 类缺陷**，均完成对应代码修复；这不是对整个项目“无剩余问题”的保证，也不是安全渗透测试。

现有 Responses 兼容目录来自较早的 `ba81c7fd758e425d67f856d601147ef0c5263d3f`。本次不整包替换它，也不把参考项目的降级规则当作所有平台都适用的标准。

## 已复现并修复

| 编号 | 影响 | 复现结果 | 修复 |
|---|---|---|---|
| A1 | 高：参数数值改变 | Trae schema/seed 中 `9007199254740993` 变为 `9007199254740992`，高精度小数被截短 | 请求使用 JSON Number，预算单独按整数校验，schema 保留原精度 |
| A2 | 高：工具调用语义改变 | 非流式 Messages 将 `{broken` 工具参数替换为 `{}`，可能让客户端执行缺参数的工具 | 无效工具参数拒绝转换，HTTP 返回 502，不伪造空输入；验证纳入尝试结果 |
| A3 | 中：坏分片被隐藏 | 聚合器跳过非 JSON 分片，夹杂错误分片也可生成成功回复 | 无效 JSON/错误分片直接报错；没有候选回复不生成成功结果 |
| A4 | 中：内容静默丢失 | 未识别的 Messages 内容块和不合法 system 对象被删除后继续请求 | 未支持内容、错误角色和 system 结构明确报错，失败发生在调用上游之前 |
| A5 | 中：并行工具协议失序 | 两个工具结果分属连续用户消息时，第一张图片被提前插入，打断连续工具结果 | 连续工具输出先组成一组，再发送相关图片/用户文本。补齐上次图片修复未覆盖的跨消息情况 |
| A6 | 中：网络错误误触发冷却 | 共享传输及 Qoder/Kiro/TeleAgent 将连接异常包装为上游 502，Trae 流读取错误失去网络类别 | 保留 TransportError 与底层错误；普通网络失败不惩罚账号/模型，真实 HTTP 429/5xx 与坏协议仍单独分类 |
| A7 | 中：客户端误判视觉能力 | CodeArts 已有 `supports_images=true`，统一目录转换时丢弃 | 保存上游明确的视觉支持标记；缺失信息不按名称猜测 |

修复前 7 个专项用例全部失败，修复后全部通过。旧测试 `TestAggregateSSESkipsGarbage` 原本明确要求跳过坏分片，已改为验证拒绝，避免继续把丢数据当兼容行为。旧 Trae 测试改用精确 JSON Number 验证预算，线上的整数值不变。

## 从 Sub2API 借鉴的处理

- [工具输出媒体](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/pkg/apicompat/responses_tool_output_media.go)：先保留连续工具结果，再将图片提升到后续用户消息。本项目用自有协议结构实现相同约束，不复制其调度系统。
- [Anthropic → Responses](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/pkg/apicompat/anthropic_to_responses.go)：明确处理图片、工具结果和思考历史，说明原生协议之间直接转换能减少中间格式丢字段。其部分未知块也会被忽略，所以不能把参考代码等同于完全无损。
- [Anthropic 流转发](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/backend/internal/service/gateway_anthropic_passthrough.go)：区分完成事件、客户端断开、读取错误和间隔超时。有些路径会在客户端断开后继续读取上游用量；本项目保持取消上游和原有预算/结算规则，不直接引入该行为。

## 仍存在的能力差距与待验证项

1. **原生协议完整性**：当前外部协议仍主要经过 Chat 中间格式，尚不能宣称支持 Claude 全部 cache_control、文档、服务端工具或 Responses 全部持久化/后台语义。已知不支持的能力应明确拒绝；完整原生透传需要单独设计和验收。
2. **平台能力目录**：部分平台没有已核实的视觉/音视频能力字段，仍保持未知。不能从型号名称直接认定支持，也不能把未知统一当成真实不支持。
3. **真实上游覆盖**：本轮使用模拟错误和隔离状态，未消耗真实账号额度。仍需针对实际客户端、型号和当前上游版本验证；不能用构建通过代替线上可用性。

其他发现没有误报为缺陷：Trae 图片输入明确返回不支持；Kiro 只接受已接入的 base64 图片、拒绝外部 URL；Messages 的不支持文档/音视频内容明确报错；Responses 的已知服务端工具与 store/background 等在入口已有显式拒绝。它们属于当前能力边界。

## 验证与交付

专项失败/修复结果分别在忽略目录外的临时日志 `/tmp/sub2api-audit-before.log`、`/tmp/sub2api-audit-after.log`。平台与协议回归、相关 race、既有 HTTP 取消/超时/错误分类、服务端无效工具参数 HTTP 回归覆盖本批变化。验收期间没有读取或修改正式运行状态、没有实际调用上游模型。

本轮保留已有 TeleAgent/WorkBuddy 未提交改动，不覆盖它们。状态 schema 仍为 10；本地代码及升级包与 GitHub 发布、系统服务部署分别管理。
