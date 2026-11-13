# R4：Qoder 工具历史兼容与当前复验

日期：2026-11-07。结论：已修复可定位的空工具历史内容兼容点，并完成当前三协议及 Pi 验收。历史 400 在本轮未复现，其全部成因不能宣布已查明或彻底消除。未部署或发布。

## 已定位并修改的内容

[chat.go](../../backend/internal/qoder/chat.go) 原先将 Chat 历史的 assistant.content=null/缺失字段直接带入 Qoder 原生请求；OpenAI 允许这种工具调用消息，而本项目既有 MIT 协议参考的结构化历史分支将其转换为空字符串。参考为 [固定版本 qoder_proxy.py](https://github.com/shuishuipingan/qoder2api-hub/blob/090cfaedb12d17418af3cbd23a59a5cae529596d/qoder_proxy.py)，不将第三方实现说明等同于官方协议保证。

新增的唯一生产行为：助手消息带非空 tool_calls，或消息角色为 tool，且 content 为 null/缺失时，转换为 `""`。非空字符串、已有空字符串、图片数组和普通助手消息不变。该处理复用 chatBody 已解码的请求私有消息，不增加共享状态或锁。

原工具调用 ID、名称、参数、reasoning_content、工具结果与顺序全部保留；没有将工具历史改成用户文本、补造推理、追加提示、裁剪结果、强制 tool_choice=none、屏蔽第二次工具调用、增加自动重试或更换原生型号。未改动 R1/R2/R3 和 Sub2API 转换复用结果。

## 基线与对照证据

保留 2026-11-06 artifacts/model-capability-audit/sources/results.json 的历史失败；本轮重新使用原 tool_case 脚本，测试 auto、dfmodel、dmodel、fast、gfmodel、kmodel、kmodel_latest、qmodel_38max 的 Chat、Responses、Messages 三种协议。

当前生产原始来源基线为 22/24：fast/Responses 和 gfmodel/Chat 第二轮重复调用，其余通过；历史 DeepSeek/Kimi 400 没有复现。初步三型号的 null/空字符串变体 6/6 通过，也保留在 diagnosis.json，不能只选择失败或成功记录。

之后固定已产生的工具历史，对 fast、gfmodel 各进行两组 null/空字符串对照，共 8 次第二轮请求。结果：null 3/4 通过，空字符串 4/4 通过；gfmodel 的一组 null 历史返回同一工具的新调用，紧接的空字符串变体消费原工具结果后结束。该结果支持补齐空值契约，但不足以证明所有间歇重复调用均来自空值；随机生成、上游处理或动态槽位行为仍可能影响结果，未通过模型自述认证实际底模。

## 隔离回归

[tool_history_test.go](../../backend/internal/qoder/tool_history_test.go) 验证空值/缺失值归一化及幂等性，非空文本、图片、普通助手消息保留。另一组测试经过 Client.Stream、COSY 编码和签名，由独立解码的模拟接收端检查：

- 首轮与续接使用同一会话/记录 ID，每次请求有独立 request_id；chat_context 继续是原用户提示，没有重复追加历史。
- 两个并行工具及倒序回传结果的 ID、名称、参数和归属保持一致，9007199254740993 不丢精度，EXACT_THOUGHT 保留。
- assistant.content=null 和已有空字符串两条路径都完成结果消费，客户端原始请求不依赖网关新增存储。

禁用新归一化函数的隔离复现用例在 null 历史处失败；启用后通过。这是针对参考兼容契约的模拟复现，不冒充历史真实 400 的直接复现。日志 /tmp/r4-reproduction.log、/tmp/r4-qoder-test.log。

完整 make check 及专项 race 通过，日志 /tmp/r4-check-final.log、/tmp/r4-race.log。原有网络、取消、错误信封、签名和结束条件测试保留。

## 新源码真实验证

临时隔离网关使用实际 qoder.Client 执行新 chatBody、归一化、COSY 签名编码和原生事件解析。测试 HTTP 接收端解码签名后的合成消息，再经生产网关原始 Chat 接口访问已授权的 Qoder 账号，将响应封装回原生事件。未读取或复制真实 Qoder 凭据，没有替换线上适配器；该中继用于验证请求语义，不用于原生网络延迟或性能结论。

共享模型固定到对应来源，不允许成功被其他平台掩盖。每项都验证首轮工具调用、结果回传、随机 CHECK 常量被读取且最终结束，保留首次结果，不以额外重试替换失败。

| 共享名称 | 固定来源 | Chat | Responses | Messages |
| --- | --- | --- | --- | --- |
| qoder-auto | qoder/cn/auto | PASS | PASS | PASS |
| deepseek-v4.1-flash | qoder/cn/dfmodel | PASS | PASS | PASS |
| deepseek-v4-pro | qoder/cn/dmodel | PASS | PASS | PASS |
| qoder-fast | qoder/cn/fast | PASS | PASS | PASS |
| glm-5.3-flash | qoder/cn/gfmodel | PASS | PASS | PASS |
| kimi-k2.8-preview | qoder/cn/kmodel | PASS | PASS | PASS |
| kimi-k3.1-pro | qoder/cn/kmodel_latest | PASS | PASS | PASS |
| qwen3.8-max | qoder/cn/qmodel_38max | PASS | PASS | PASS |

24/24 通过。解码后的第二轮请求逐项检查工具 ID 对应、空内容归一化和完整历史；没有本地转换产生的重复追加。覆盖的是 8 个历史受影响 CN 槽位，不是所有 Qoder 地区、账号和目录型号的全面稳定性保证。

Pi 使用临时 agent 配置、openai-responses 协议、共享名称 qoder-fast，仅启用 read，完成读取合成文件并返回随机内容，1/1 通过。真实 Pi 配置未改写；临时配置自动清理。

本轮真实请求共 115 次：初步诊断 9 次、生产基线 48 次、修复后隔离中继 48 次、Pi 2 次、固定历史对照 8 次；都经过原有生产权限、计费与健康机制。一次隔离测试初始化遗漏账号 ExternalID，导致测试账号身份拒绝及后续 503，实际没有转发上游；修正测试数据后重跑，原失败单独保存在 fixture-identity-error-*，不计入上游能力结果。

证据保存在本机忽略目录 artifacts/r4-qoder-tools-2026-11-07/：baseline-results.json、diagnosis.json、paired-results.json、results.json、summary.json、pi-results.json、forwarded.jsonl 和复验脚本。forwarded.jsonl 仅含本批合成消息与临时测试路径，不含生产认证信息；不进入 Git。

## 未确认事项和部署

历史 DeepSeek/Kimi HTTP 400 的原始供应商错误正文没有可用证据，本轮也未复现，归因仍未确认；不能只因现有用例通过就宣称历史所有 400 都被本补丁修复。模型自主再次读取工具也不是网关可以一律禁止的行为。当前没有证据支持更改 chat_context、增加工具强制规则或给每个平台增加推理缓存，因此没有实施这些变更。

备份：/home/white/backups/codearts2api/before-r4-20261107-191034/source-changes.tar.gz，另存 Git HEAD，不含生产状态。codearts2api.service 仍使用原 PID 2102403，2026-11-06 22:29:46 CST 启动。本批源码尚未部署、推送 GitHub或发布 Release。R4 的当前兼容补丁及验收完成，与历史所有异常根因查明分别记录；R5、R6 未包含在本批。
