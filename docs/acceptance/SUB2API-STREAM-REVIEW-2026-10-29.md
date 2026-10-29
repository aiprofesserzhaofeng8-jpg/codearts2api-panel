**响应保真与速度复核：对照 Sub2API · 2026-10-29**

结论：新增“只读、轻量、分协议”的响应校验没有显示出秒级 CPU 开销，但尚未作为正式实现上线，不能承诺所有真实请求零误伤。当前项目本身已存在更值得优先处理的文本清洗范围过宽和聚合字段丢失；这两项已经在本地复现。普通 CodeArts 流式路径能在上游结束前转发首个内容分片，摘要保护路径则有意等整段生成完成。

本次只检查源码、运行 Go overlay 下的模拟测试和微基准；没有改动生产业务代码或配置，没有调用真实模型/搜索，没有运行 Sub2API 服务的性能测试。

**参考版本与做法**

参考上游 `Wei-Shaw/sub2api` 在查询时 main 指向的提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2`，提交时间 2026-10-28T13:41:00Z。本结论不自动适用于本机安装的其他 Sub2API 版本，也不表示该项目所有平台/协议实现完全一致。

| 场景 | 核实的 Sub2API 行为 | 对本项目的意义 |
|---|---|---|
| Anthropic 非流式 | 读取受限大小的 body；解析失败的 2xx 在提交下游前转为 failover；正常响应继续使用原 body，必要的模型/用量/工具名修改单独处理 | 校验与重构正文分开，不为了验证而用固定小结构重建全部响应 |
| OpenAI Responses 流式透传 | 首次语义输出前缓存前导元数据事件；输出开始后在 SSE 完整事件边界 Flush；可配置心跳；测试断言原事件内容和刷新边界 | 不等整段生成再返回，也不逐字符解析重建；前导缓存与全文缓冲不能混为一谈 |
| 空完成检测 | 特定检测器同时检查正文、工具、旧式 function call、usage、错误、推理和结束原因 | 不能只因没有文字就判异常，也不能因 finish_reason=length 就丢弃已有输出 |
| OpenAI Responses 输出识别 | refusal、工具参数、推理、未知内容项有专门识别或保守放行路径 | 已知合法非文本输出和未来扩展字段必须有保真边界 |
| 非流式上游却返回 SSE | OpenAI 透传路径识别 SSE，优先采用终止事件的完整 response，必要时从增量补齐输出 | 应做明确的协议恢复，不把 SSE 文本伪装成 JSON，也不要仅抽 content 字符串 |
| 已有输出后的失败 | 区分下游是否已有语义输出、心跳与终止事件，防止可见输出后任意重放 | 一次校验失败不能触发不受限的网关/客户端叠加重试 |

源码与测试：

- [Anthropic 非流式实现](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/gateway_upstream_response.go#L1397)，[非 JSON/有效 JSON 回归测试](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/gateway_non_streaming_response_test.go#L40)。
- [流式透传及 SSE→JSON](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/openai_gateway_passthrough.go#L1872)，[事件刷新与失败边界测试](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/openai_gateway_passthrough_flush_test.go#L107)。
- [空完成检测器](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/openai_silent_refusal.go#L110)。

不能据此宣称 Sub2API 对所有非流式响应都有统一且严格的 schema 校验。查看到的 OpenAI 透传路径和 Anthropic 路径采取不同处理；某些路径还有针对客户端的字段修正及断连后继续读取 usage 的策略，这些取舍不能整套照搬。

**速度实测**

CPU：Intel i5-13400F；Linux amd64；Go 1.27.3。使用以 ASCII 文本为主的合成 JSON，三个独立 benchmark 运行结果取中位数，未启用 race 仪器。响应校验是评估用原型，仅验证 JSON、error 信封和 choices/message 外壳，成功时原 body 不变；请求清洗则调用当前实际 `PrepareBody(...,passthrough,...,sanitize=true,codearts)`。两列处理对象不同，用于判断开销量级，不是同功能优化前后对照，更不是 Sub2API 与本项目的整机性能比较。

| 数据正文尺寸 | 只读响应校验原型 | 当前请求清洗 | 当前清洗每次累计分配 |
|---|---:|---:|---:|
| 1 KiB | 0.0028 ms | 0.0494 ms | 约 11.3 KiB |
| 1 MiB | 1.5187 ms | 38.6275 ms | 约 9.9 MiB |
| 8 MiB | 7.8101 ms | 310.0083 ms | 约 80.1 MiB |

分配量是 B/op，不是进程峰值内存或泄漏。大请求清洗会增加首字前处理时间；并发可能放大 CPU、分配与 GC 压力，但本次没有证明它已造成线上秒级卡顿。固定 CPU、序列化内容和负载限制了数据外推范围。

另外使用阻塞上游结束事件的本地测试验证：普通 `/v1/chat/completions` 流式请求在上游尚未完成时已收到 FIRST 分片，单次测试耗时约 0.892 ms。这个值不是模型首 Token 时间或延迟百分位，只证明该路径没有等待整段答案。摘要路径见 [summary.go](../../internal/server/summary.go#L28)，它主动缓存完整摘要；摘要生成及重试等待不能归因于 JSON 校验。

**已复现的数据变更**

1. WorkBuddy 清洗开启、提示词为 passthrough 时，用户消息 `Keep ID 11128 exactly` 被改成 `Keep ID 11-128 exactly`。这不是仅替换客户端身份模板，而是改动业务文本。默认 custom 还有删除原 system/developer 的独立行为。
2. 三个平台共同使用的清洗规则，会把历史工具参数字符串 `cc_version=42; keep me 11128` 中的 `cc_version=42;` 删除。WorkBuddy 还会改写后面的数字字符串。即便规则原本针对客户端元数据，也无法仅凭这个文本片段判断它是否是用户需要保留的代码/内容。这里改变的是发往上游的上下文，没有执行任何真实工具或写入用户文件。
3. `AggregateSSE` 接收合法的 `delta.refusal` 与正常 stop 事件时，最终结果中没有 refusal，而是空的 content。该聚合器用于流转非流的若干路径；这证明该字段在该路径丢失，不表示所有协议或普通透传路径都丢失同一字段。

定位：[清洗规则](../../internal/requestpolicy/sanitize.go#L85)、[清洗工具参数](../../internal/requestpolicy/sanitize.go#L160)、[聚合器固定字段](../../internal/codearts/sse.go#L41)。完整复现输出见 [reproductions.log](evidence/sub2api-review-2026-10-29/reproductions.log)。

评估原型对工具调用无文本、refusal、length 截断、未知字段、未知结束原因、大整数扩展字段分别验证了接受且原字节不变。这仅覆盖这些合成样本，不能把原型直接视为可投产的完整校验器。

**建议采用的修复边界**

- 同协议成功响应尽量保留原始字节。校验只提取必要结构，不过滤未知字段，不重新生成整个 JSON。
- 非流式只对明确无效的结果报错；对工具调用、拒绝、合法截断等分别处理。需要协议转换时，先定义字段保留/显式不支持的矩阵。
- 流式保持逐事件转发；首个有效输出后禁止回到起点重试。不要为校验把普通流式改成整段缓存；不在每个分片同步写盘。
- 清洗默认不修改 user、tool、历史工具参数等业务数据。只对可识别的客户端元数据应用明确规则；完整文本清洗作为单独可选模式，明确代价。优先取消对普通业务字符串 `11128` 的全局替换。
- 修复聚合器对 refusal 等字段的遗漏，并测试多个 choices、工具参数分片、usage-only 帧、未知结束原因和扩展字段。对于无法保真转换的内容应明确声明能力边界，不能静默删掉。
- 上线前以同一批样本对比原始响应、转换后语义、首分片时刻、总耗时和实际上游次数；观察模式只记录结果元信息，不保存私有对话正文。

本次产物：[benchmark.log](evidence/sub2api-review-2026-10-29/benchmark.log)、[结构化统计](evidence/sub2api-review-2026-10-29/benchmark-summary.json)、[测试源](evidence/sub2api-review-2026-10-29/assessment_test.go.txt)。
