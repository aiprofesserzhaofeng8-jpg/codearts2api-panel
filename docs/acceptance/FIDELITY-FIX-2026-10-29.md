**第一批保真修复 · 2026-10-29**

基线为 `bdcb98b`，标签 `snapshot-20261029-220947-before-quality-fixes`。本轮只收窄清洗并修复 Chat 流转非流聚合；未引入新依赖、额外上游调用、自动重试或普通流式缓冲。

- 清洗只针对 system/developer content 中两条客户端身份模板和独立计费头行。用户、tool、历史工具参数与推理内容原样保留；裸 `cc_...` 和 `11128` 不再改写。未知 content 形态保留，JSON 扩展整数不转成浮点。
- passthrough 没有发生修改时直接返回原字节；custom 仍按用户选择替换系统提示词，各平台默认模式不变。
- 聚合器按 choice index 保存独立状态，保留 `refusal` 及它的空字符串存在性，工具 index 在各 choice 中隔离；正文、推理、结束原因和 usage 继续保留。
- 保持显式字段结构，不声称任意扩展字段或所有跨协议转换已完全无损。后续响应结构校验、监控和持久化建议没有纳入这一批。

参考 [Sub2API 非流式正常正文保留测试](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/gateway_non_streaming_response_test.go) 和 [逐事件刷新测试](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/openai_gateway_passthrough_flush_test.go) 的保真原则；未复制其框架或增加第三方运行依赖。

**性能对照**

同机 Intel i5-13400F、Go 1.27.3，修复前后使用相同的合成工作负载，每项运行三次，取中位数。请求清洗用小 system + 长 user，不命中模板；聚合用单 choice、每个分片 32 字符。不是实际模型速度，也不是吞吐上限。

| 工作负载 | 修复前 | 修复后 |
|---|---:|---:|
| 1 KiB 请求清洗 | 0.0489 ms | 0.0081 ms |
| 1 MiB 请求清洗 | 38.8930 ms | 3.3714 ms |
| 8 MiB 请求清洗 | 303.9852 ms | 23.0816 ms |
| 聚合 100 个正文分片 | 0.1232 ms | 0.1139 ms |
| 聚合 2000 个正文分片 | 2.1313 ms | 2.3885 ms |

1 MiB 清洗累计内存分配从约 9.87 MiB 降至 2.04 MiB；8 MiB 从约 80.10 MiB 降至 16.05 MiB。2000 分片聚合增加约 0.26 ms，分配从约 704 KiB 增至 737 KiB；这是增加独立候选状态与字段保留的局部成本，不是进程内存泄漏。

证据：[统计](evidence/fidelity-fix-2026-10-29/benchmark-summary.json)、[修复前](evidence/fidelity-fix-2026-10-29/before-benchmark.log)、[修复后](evidence/fidelity-fix-2026-10-29/after-benchmark.log)。基准测试分别位于 `internal/requestpolicy/benchmark_test.go` 和 `internal/codearts/sse_benchmark_test.go`。

**验证**

- `go test -race -count=1 ./...` 全部通过；`go vet ./...`、JS/CSS 检查和 diff 格式检查通过。
- 回归覆盖业务文本与历史工具参数保留、system text block 扩展字段及大整数、未知 content 形态、无修改时原字节返回、清洗幂等、多个 choice 及同 index 工具隔离、refusal、usage-only 帧、length 与未知结束原因。
- CodeArts/WorkBuddy Chat 流式及非流式实际处理路径均验证 refusal 保留、单次调用。独立 HTTP 服务测试主动扣住上游结束事件，客户端先收到内容分片后才放行结束，以确认没有整段等待。
- 独立空账号实例运行两个浏览器 smoke，验证三个平台开关、保存/重载、布局及 WorkBuddy 兼容配置；测试完成后恢复默认配置。
- 本次没有调用真实模型或搜索。收窄清洗后，上游仍可能对保留下来的业务文本拒绝；不承诺放行率不变。若发生拒绝，应按实际错误判断，不能以改动用户业务文本来掩盖。

[全量竞态结果](evidence/fidelity-fix-2026-10-29/race.log)；[源码指纹与验证记录](evidence/fidelity-fix-2026-10-29/verification.json)。
