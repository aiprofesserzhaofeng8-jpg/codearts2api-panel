**第一批保真修复独立复测 · 2026-10-29**

结论：第一批承诺的代码行为达到预期。运行程序与修复版构建的 SHA256 一致；对业务数据保留、拒绝内容聚合、多候选隔离、流式及时返回及性能进行了本次重新执行的验证，没有借用旧测试日志当作新结果。新增一个持续保留的测试文件 `internal/server/fidelity_acceptance_test.go`，没有修改业务逻辑或运行配置。

| 验收项 | 方法 | 结果 |
|---|---|---|
| 运行版本 | `/proc/<pid>/exe` 与 bin 文件比较；复测前源码指纹与修复版验证记录比较；只读 /health | 一致，HTTP 200 |
| 三个平台业务内容 | CodeArts、WorkBuddy、Trae × passthrough/custom × 流式/非流式，检查真实适配器发往模拟上游的 payload | 12/12 通过 |
| 用户文本 | 带 11128、cc_version、独立计费头文本、CLI 身份句、换行和首尾空白的 user 消息 | 原文一致 |
| 历史工具参数与工具结果 | 检查 path/content、JSON 字符串及 9007199254740993、调用 ID、工具结果 | 内容一致；原生协议形态转换允许，业务值不变 |
| 重试与租约 | 每组核对实际请求数、attempts、用量与最后 in-flight | 均只调用一次、用量有回执、租约归零 |
| refusal | CodeArts/WorkBuddy Chat 流式及非流式，共 4 组，重复 20 轮 | 80/80 子案例通过 |
| 普通流式及时返回 | 阻塞模拟上游完成事件，只有客户端收到 FIRST 内容才能放行结束 | 连续 20/20 通过 |
| 多候选隔离 | 正文、推理、不同 choice 下相同 tool index、refusal、length/未知结束原因、usage-only | 本次全量回归通过 |
| 全量竞态 | `go test -race -count=1 ./...` | 所有测试包通过，无竞态报告 |
| 静态检查 | go vet、JS/CSS 检查、git diff --check | 通过 |

新测试不仅检查 HTTP 200，而是在适配器出口解析 user、assistant 历史调用及 tool 消息，直接比较内容。Trae 会按其原生协议将字符串改为 text block、function 改为 function_call，测试检查这些转换后承载的值没有变化。

**负向对照**

通过 Go overlay 临时恢复固定点 `bdcb98b` 的清洗、策略调用和聚合器，同一组断言在旧代码上出现预期失败：12 组业务内容检查全部失败，两个非流式 refusal 案例失败。普通流式 refusal 两个案例仍通过，符合原来只在聚合路径丢字段的定位。该对照没有覆盖或回滚当前工作区/运行服务；失败意味着测试成功检出了旧缺陷。

**本次性能复测**

同一基准工作负载，500ms benchmark 窗口，每项三次取中位数；基线数据为此前固定版的同机测量。

| 工作负载 | 固定版基线 | 本次复测 |
|---|---:|---:|
| 1 KiB 请求清洗 | 0.0489 ms | 0.0075 ms |
| 1 MiB 请求清洗 | 38.8930 ms | 3.0329 ms |
| 8 MiB 请求清洗 | 303.9852 ms | 23.2379 ms |
| 聚合 100 个正文分片 | 0.1232 ms | 0.1005 ms |
| 聚合 2000 个正文分片 | 2.1313 ms | 1.8957 ms |

清洗开销明显下降。聚合上轮测得约 2.39ms、本轮约 1.90ms，处于微基准运行波动范围；应解读为没有观察到明显变慢，不宣称存在确定的聚合加速。B/op 仍有少量增加，用于独立候选状态和 refusal 保留。上述数据不是实际上游首 Token 时间，也不是高并发容量证明。

**边界**

没有使用真实账号发送生成或搜索请求，也没有消耗真实模型积分。测试证明本次修复的数据处理及受控流式行为，尚不能证明真实上游放行率保持不变、任意扩展字段全部无损、Trae 原生 refusal 能力，或所有跨协议 refusal 映射。后续响应校验、监控修复等不属于这批验收内容。

证据：[运行版本](evidence/fidelity-retest-2026-10-29/live.json)、[平台矩阵](evidence/fidelity-retest-2026-10-29/provider-matrix.log)、[旧版本负向对照](evidence/fidelity-retest-2026-10-29/baseline-negative-control.log)、[20 轮流式测试](evidence/fidelity-retest-2026-10-29/stream-repeat.log)、[竞态结果](evidence/fidelity-retest-2026-10-29/race.log)、[性能统计](evidence/fidelity-retest-2026-10-29/benchmark-summary.json)。
