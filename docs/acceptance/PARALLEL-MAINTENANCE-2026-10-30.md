**响应校验、取消恢复与关闭思考映射 · 2026-10-30**

三项工作已分别实现、合并并部署。完整竞态测试、vet、隔离浏览器验收通过；真实上游 31 个检查全部通过。冻结回退点仍为 `snapshot-20261030-020016-verified-tools`。没有修改生产默认提示词、清洗策略、重试次数、超时设置或账号调度参数。

**修改范围**

| 工作 | 确认的问题 | 实现与边界 |
|---|---|---|
| CodeArts 原生非流式响应 | `null`、空 choices、缺失 message、标准 error 信封可能被当成成功。旧版 Chat/Messages 的 8 个集成样本复现 HTTP 200；Responses 旧转换器本身已能发现部分异常 | 仅校验 Chat 外壳；与已有 error_code 分类合并解析，删除成功路径重复扫描。正常响应返回原字节，保留工具、refusal、推理、截断、未知字段、大整数和未知结束原因。不强求文字、usage 或 finish_reason 非空。WorkBuddy/Trae 原生 SSE 聚合未套用这个新增校验器 |
| 三平台取消/超时 | WorkBuddy/Trae 在错误响应正文尚未读完时取消，会先执行 401/429 故障分类，误罚账号或模型；8 个失败案例已复现 | 将共享重试器已有 context 检查前移至错误分类前；保留取消解绑。成功请求分支不增加工作，不扩大重试、不新增等待 |
| Messages 思考偏好 | 入站 DTO 丢弃 thinking，客户端指定 disabled 也未送往上游 | CodeArts/WorkBuddy 显式 disabled 映射到原生开关；省略参数保持原样。完整 thinking 支持尚未完成，不能宣称已支持开启、自适应或预算换算 |

业务文件共六个：`internal/codearts/client.go`、新增 `internal/codearts/nonstream.go`、`internal/protocol/anthropic.go`、新增 `internal/protocol/anthropic_thinking.go`、`internal/server/anthropic.go`、`internal/server/chat.go`。其余变更为测试、基准和验收材料。

思考输入有明确的兼容性变化：Messages 的 enabled、adaptive、budget_tokens、output_config.effort 以前被忽略，现在返回 HTTP 400；Trae 的显式关闭同样在获取账号前返回 400，表示网关尚未实现映射。默认携带这些选项的 CLI 需要省略相应选项。Trae Chat/Responses 的既有处理保持；本轮没有运行实际 CLI，不能承诺所有客户端默认参数均已适配。详见 [思考映射矩阵](MESSAGES-THINKING-MAPPING-2026-10-30.md)。

**参考 Sub2API 的方式**

参考固定提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2`，并非声称其所有路径都有统一的严格校验：

- [非流式响应处理](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/service/gateway_upstream_response.go)：失败应在提交下游前识别，正常正文保留。我们的外壳校验针对本项目已复现的缺陷，没有整套复制内容过滤。
- [取消与重试边界](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/handler/openai_gateway_handler.go)：取消后不应开始新的换号重放。其部分路径断连后继续读取用量，本项目保留原有取消上游的方式，不引入继续生成的积分消耗策略。
- [Anthropic 协议桥](https://github.com/Wei-Shaw/sub2api/blob/a3298b46ebe239fb0720b068a1eb3383955bdac2/backend/internal/pkg/apicompat/chatcompletions_anthropic_bridge.go)：参数在转换边界处理。三个上游参数能力不同，本轮只补有依据的关闭映射。

**本地回归与性能**

完整 `go test -race -count=1 ./...` 通过，耗时约 51.6 秒；go vet、构建、最终定向回归、diff 检查通过。无账号隔离实例的设置保存/恢复、明暗主题和移动布局浏览器测试通过，实例已停止。取消专项 61 个双 HTTP 案例覆盖三平台、三协议、双模式、实际 1 秒配置超时、等待响应头、错误正文中断及下一请求恢复，见 [取消验收](CANCEL-TIMEOUT-2026-10-30.md)。

新增受控并发测试使用真实 localhost 客户端和上游连接，检查 1/4/16 并发 × 流式/非流式，合计 126 次请求；逐片内容、完整结束、用量、请求数与租约均符合预期。既有 100 请求/2 并发测试加强为完整 JSON/choices/finish 断言，并显式选择支持原生非流式的模型夹具；旧测试的上游 SSE 原本对应 StreamOnly 模型，不能将它描述成非法上游夹具。

同机、相同基准、冻结版与修改版分别运行，`-cpu=1`、无 race、每项三次各 300ms，取中位数。下面是本地处理开销，不包含模型、网络或排队：

| 路径 | 修改前 | 修改后 | 变化 |
|---|---:|---:|---:|
| CodeArts 非流式签名、读 body、校验和用量捕获，约 1 KiB | 0.024625 ms | 0.024215 ms | -0.000410 ms |
| 同路径，约 1 MiB | 3.177004 ms | 2.766074 ms | -0.410930 ms |
| 同路径，约 8 MiB | 25.047825 ms | 21.851911 ms | -3.195914 ms |
| Messages 短请求，未指定 thinking | 0.001373 ms | 0.001376 ms | +0.000003 ms |
| Messages 短请求，显式 disabled | 0.001531 ms | 0.002370 ms | +0.000839 ms |
| Messages 16 KiB 请求，未指定 thinking | 0.029513 ms | 0.029514 ms | +0.000001 ms |
| Messages 16 KiB 请求，显式 disabled | 0.029669 ms | 0.030684 ms | +0.001015 ms |
| 100 个 Messages 正文分片 | 0.270761 ms | 0.270949 ms | +0.000188 ms |
| 100 个 Messages 工具分片 | 0.343950 ms | 0.344919 ms | +0.000969 ms |

未指定 thinking 的分配次数不变，DTO 多 48 字节；显式 disabled 增加约 888 字节、10 次小分配。流式基准分配量与次数均相同。本批没有显示明显的正常路径退化，但不能用微基准承诺所有硬件或负载绝对零增量。没有新增普通流式全文缓存、逐片完整 JSON 校验、逐片写盘或额外上游尝试。

**真实上游验收：31/31**

使用三平台 DeepSeek V4.1 Flash，专用临时密钥限制最多 40 次客户端请求，单次参数最多设置 512 输出 Token。测试后删除密钥，不将参数上限称作上游实际扣费硬上限。

| 检查 | 数量 | 结果 |
|---|---:|---|
| 三平台 × Chat/Responses/Messages × 流式/非流式 | 18 | 全通过；随机业务字符串完整，结束语义、路由、用量及单次尝试正确 |
| CodeArts/WorkBuddy Messages 显式 disabled × 双模式 | 4 | 全通过；返回正常，四次回执 reasoning_tokens 均为 0。这是样本结果，出站字段保真由受控 HTTP 测试证明，不外推到所有模型 |
| 不支持的 Messages 思考输入 | 3 | 预期 HTTP 400；发生于账号/上游调用前 |
| 三平台 Messages 流式两个并行工具 | 3 | 两个独立工具块，真实并行读取合成文件、倒序回填，返回值正确 |
| 三平台真实流中主动断开并立即发起恢复请求 | 3 | 看到正文后断开；取消请求一次尝试，账号/模型健康不变、租约释放，随后立即恢复成功 |

共 37 次客户端请求，日志对应 34 次上游尝试，没有重试；3 个不支持输入由网关直接拒绝。官方 Anthropic Python SDK 0.86.0 离线解析全部 11 条真实 Messages 流，均通过。普通/关闭思考的 22 个文本检查本批均严格匹配随机字符串；此前 WorkBuddy Markdown 包装行为按决定保留，没有加剥离规则。

已获得的上游回执合计输入 9,227、输出 1,329 Token，缓存输入 3,584、推理输出 504 分别已包含在输入/输出中。三个取消请求没有最终用量，日志正确标记未知；这些合计不包含未回报的实际消耗，不能当作完整积分账单。取消仍沿用原有 502 日志分类，本轮没有新增独立取消统计。

生产总超时仍保持原配置，没有为真实验收等待数分钟制造超时；超时、401/429 错误体中断与先失败再恢复由受控真实 socket 测试验证。没有高并发调用付费模型，也没有覆盖所有型号、实际 CLI、外部搜索和令牌续期中断。

**部署与回退**

部署前在途为 0。新的运行程序 SHA256：`08ce2f9deaf028a7c19b9e5a7791468d3b494f15efd74d9ecdc47c162259ece3`。当前源码来自冻结提交 `bd2196b522233a67285aa39e0500e1d052d38a52` 加本轮改动；真实测试期间尚未提交，源码业务文件逐一摘要见 verification.json，不把旧 HEAD 冒充修改后的完整源码身份。

最终业务源码提交为 `830aa51a8b46479e761b8bb90532aa9c4f8a4e01`，其中响应校验 `729e0f2`、取消分类 `3591012`、关闭思考 `830aa51` 分别提交，便于独立审阅与回退。

本轮程序与状态备份：`data/backups/before-parallel-maintenance-20261030-021423/`。普通回退只恢复程序，保留当前凭据、用量与预算。真实测试结束健康 HTTP 200、在途为 0，设置摘要和账号数量保持不变，临时密钥已删除。

更细的质量计时、用量持久化改造、所有 thinking 模式、实际 CLI 和更大容量验收仍是独立后续工作，本轮没有将这些宣称完成。

证据：[完整 race](evidence/parallel-maintenance-2026-10-30/race.log)、[最终定向回归](evidence/parallel-maintenance-2026-10-30/final-targeted.log)、[性能统计](evidence/parallel-maintenance-2026-10-30/benchmark-summary.json)、[真实响应](evidence/parallel-maintenance-2026-10-30/live.json)、[用量与 SDK 核验](evidence/parallel-maintenance-2026-10-30/verification.json)、[部署记录](evidence/parallel-maintenance-2026-10-30/deployment.json)、[独立真实执行脚本](evidence/parallel-maintenance-2026-10-30/live-runner.py.txt)。
