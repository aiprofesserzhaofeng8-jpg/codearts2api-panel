**多工具异常的根因调查 · 2026-10-30**

结论：Messages 流式串工具参数的根因已经确定，是共享转换器的状态模型错误；默认提示词与严格 JSON 指令冲突也得到请求侧和原始响应侧证实。Trae 历史 12 调用已经排除网关重试，源码 ID 来源分析指向原生上游，但原请求没有保存原生事件，三次新请求也未再次出现，因此不能声称已经还原那一次上游内部为何重复。

业务代码、生产设置和运行服务均未修改。调查包含源码/Git 历史、10 个本地竞态诊断案例，以及 9 次隔离进程中的真实请求。真实请求只使用此前的合成测试上下文；临时 Store 使用复制的凭据，关闭刷新、奖励、后台任务和重试，不写生产状态。没有收集或保存认证请求头。

**1. 确定的网关根因：把内容类型当成工具身份**

位置：`internal/server/anthropic.go:186` 的 anthropicStream、`:221` 的 openBlock 和 `:286` 的 consume 工具循环。

状态仅包含一份 index/kind/toolID/toolName/toolArgs。多个工具需要独立的状态，但这里实际上只能表示当前一个工具：

1. 工具 A 到来，toolID=A，openBlock("tool_use") 创建内容块 0，并发送 A 的参数。
2. 工具 B 到来，consume 将 toolID 改为 B，清空内部 toolArgs，调用 openBlock("tool_use")。
3. openBlock 只比较 kind。当前已经是 tool_use，因此立即返回；没有发出 A 的 stop，也没有创建 B 的 start。客户端仍只认识 A 的内容块。
4. B 的参数继续写往同一个内容块 index，客户端拼出 `{"path":"left.txt"}{"path":"right.txt"}`；B 的 ID/名称没有被正确声明。
5. 后续分片通常只带 tool index、不再带 ID。代码虽然解析了 tc.Index，却未使用它路由分片；所以多个工具增量交错时也不能找到正确归属。

仅在“ID 变化时强制新开块”不足以完整处理交错分片：必须按上游工具 index/ID 保存归属，并正确管理输出块生命周期。该结论来自当前状态变量和分片消费逻辑，不是已实现的新方案。

**触发范围与因果回放**

固定的相同上游分片经过 Chat、Responses、Messages 的流式/非流式六条路径，只有 Messages 流式从 2 个工具变成 1 个且参数非法。额外四个状态机案例进一步缩小了原因：

| 输入 | 输出工具块数 | 参数合法数 | 解释 |
|---|---:|---:|---|
| 单工具参数分片 | 1 | 1 | 原状态模型足够 |
| 两工具各带完整参数、依次到达 | 1 | 0 | 无需分片交错也会触发 |
| 两个工具头后交错到达参数 | 1 | 0 | 未使用工具 index，参数进一步混合 |
| 两工具之间插入正文文本 | 2 | 2 | kind 被切换，意外绕过缺陷 |

故障不依赖真实网络、模型采样或压测并发。三个平台都走同一转换器，因此同时受影响。非流式使用 AggregateSSE 按工具 index 聚合，再使用 FromOpenAI 转换；Responses 有自己的状态管理，没有经过这个故障分支。

**为什么错误还会正常结束**

toolArgs 虽然不断累积，但没有被用于完成时的 JSON 校验；finish 根据 sawToolUse 发送 tool_use 结束原因和 message_stop。因为转换器没有返回错误，streamAnthropic 也把这次记为成功。外层 SSE 每个 JSON 都合法，真正非法的是客户端拼接后的内层工具参数，所以此前 Client.Chat 的非流式 JSON 语法检查不能覆盖它。

**为什么以前没有暴露**

Git blame 显示 openBlock 的同类型直接返回、单份工具状态以及 consume 的工具分支从初版 `ab129f3`（2026-10-16）就存在。近期的 JSON 校验、监控统计修改没有更改这些行。现有工具转换测试及前一轮基础验收主要覆盖单工具；单工具或工具间插入正文时恰好正常。普通测试全部通过不能证明多工具的流式 HTTP 编码正确。

**2. Markdown 包装：上游生成，提示词策略会影响格式遵循**

在 WorkBuddy 原生 HTTP 响应、归一化 Chat 分片、最终 Messages 正文三个边界同时记录文本，出现代码块时三者内容一致，证明转换器没有新增 Markdown 包装。

请求侧同时证实：`requestpolicy.PrepareBody` 的 custom 模式会删除客户端 system/developer，插入配置的系统提示词。当前默认模板明确要求“用 Markdown 组织结构”和“代码块标注语言”。所以即使客户端 system 要求只返回原始 JSON，在 custom 模式下该系统指令也不会送到上游；用户消息中的 JSON 要求仍在。这是当前显式策略产生的指令取舍。

用同一份已经完成工具调用的合成上下文，客户端 system 统一要求原始 JSON，temperature=0；仅在隔离 Store 中修改模式，各运行两次：

| 隔离策略 | 客户端 JSON 系统指令是否保留 | 默认 Markdown 两条规则是否存在 | 出现代码块 |
|---|---|---|---|
| 原有 custom | 否 | 是 | 1/2 |
| custom，仅删除两条 Markdown 格式规则 | 否 | 否 | 0/2 |
| passthrough，保留客户端 JSON 系统指令 | 是 | 否 | 0/2 |

所有六次内容值都正确。小样本支持提示词冲突是影响因素，但不能建立长期概率结论，也不能说明 custom 必然加代码块。此前 CodeArts passthrough 同样有一例代码块，说明模型自身的格式遵循也有波动。未使用 response_format/json_schema 等结构化输出约束，单靠自然语言不能承诺严格 JSON 文本。

最终 API 的外层响应 JSON 正常，偏差发生在 message.content 文本中；因此不应把合法外层响应判为非法 JSON，或全局剥离用户可能需要的代码块。

**3. Trae 历史 12 调用：能确认到哪一层**

原记录只包含一次上游尝试，却返回 12 个不同 tool_use ID，前缀依次为 call_00 至 call_11。已经排除“网关重新执行请求 6 次”。

沿 ID 数据流核对：Trae toolDeltas 在存在 ID 时复制上游 ID；缺失时才生成 `call_` 加 32 位十六进制。AggregateSSE 和 FromOpenAI 的缺 ID 补值也使用随机十六进制形式。原 12 个 ID 均不是这些补值格式。源码没有把两个调用扩展成 call_00 至 call_11 的生成分支，已有非流式 FromOpenAI 只是逐个映射数组项。这是“12 个独立调用身份来自原生上游”的强数据流证据。

使用同一历史请求体新增 3 次真实原生抓取，结果均为：原生 SSE 2 个不同 ID → Trae 归一化分片相同 2 个 ID → 最终非流式 Messages 相同 2 个 tool_use；每次都只发送一个 HTTP 请求。未观察到适配器放大工具数。

因此当前证据指向 Trae 原生服务/模型偶发返回过多调用，而非已经确认的流式块合并缺陷。但历史那一次原生 SSE 没有保存，不能事后补造，也无法继续区分 Trae 服务端组装和模型生成内部的具体原因。三次未复现不等于它不再发生。

**补充发现：测试中的思考参数没有贯穿 Messages 请求**

历史脚本向三个接口都写入 thinking.type=disabled，但 protocol.Request/ToOpenAI 仅支持列出的字段，没有 thinking，因而 Messages 会忽略这项输入。实际抓取的 Trae 请求中确实没有 thinking，max_tokens=640 则被保留并发往原生服务。需要修正之前的测试解释：Messages 场景不能说已关闭模型思考。

该参数缺口不能单独解释并行参数合并或历史 12 次调用。历史 927 个输出 Token 超过设置值 640，而新抓取分别回报 83、85、85，不能凭本次结果解释历史上游计量/限额语义；目前仅能排除“网关把 max_tokens 改大或丢弃”。

**修复应如何限定范围**

优先只修 Messages 流式转换器的工具块归属和结束边界，覆盖同名多工具、交错分片、ID 只在首片出现、工具间文字及结束事件。不需要改变账号池、重试上限、请求清洗或默认提示词。普通正文继续逐事件发送，不能为了这个问题缓存整段回答。提示词模式和 Trae 的多调用限制应独立评估，避免把三类原因混成一个修复。

本次 9 次真实诊断回报输入 6,243、输出 513 Token，记录只保存在隔离诊断产物中，没有写入生产请求日志或预算。没有执行任何工具副作用、刷新凭据或奖励操作。结束时生产配置摘要与二进制一致、在途为 0，健康检查 HTTP 200。

证据：[分层真实抓取](evidence/tools-root-cause-2026-10-30/native-traces.json)、[抓取摘要](evidence/tools-root-cause-2026-10-30/summary.json)、[真实诊断脚本](evidence/tools-root-cause-2026-10-30/native_trace_test.go.txt)、[10 个本地诊断](evidence/tools-root-cause-2026-10-30/local-causes.log)、[状态机诊断源](evidence/tools-root-cause-2026-10-30/block_cause_test.go.txt)、[运行核对](evidence/tools-root-cause-2026-10-30/runtime.json)。诊断 PASS 表示证实当前行为/错误，而非修复通过。
