**可用账号数统计修复 · 2026-10-29**

仅修改业务文件 `internal/server/metrics.go`：将 `codearts2api_accounts_ready` 的计数来源改为既有 `Store.PoolSnapshot()` 中的 ready 状态。删除健康状态分支中的一次计数，增加 5 行快照统计，并更新指标说明。没有修改账号池、选号、请求转发、重试或任何上游适配代码。

账号级可调度口径：启用、未暂停、不在账号级冷却中，且未达到有效并发上限。有效上限沿用账号池的独立账号配置或全局默认。原有 `codearts2api_accounts{status="ok|error|disabled"}` 保留健康分类；历史 error 状态本身不阻止账号池重试，不能用它代替调度状态。该全局计数不包含模型冷却、供应商/模型匹配、分组授权等特定请求条件，也不保证凭据或上游一定可用。

验证：

- 定向竞态测试通过：正常、停用、暂停及恢复、冷却与过期、默认并发满、独立并发满、独立并发未满、释放后恢复、历史错误可重新调度；逐项与真实 AcquireAccountForModel 的结果对照。
- 单独确认模型冷却只限制对应模型，不能将整个账号从全局计数中移除；健康分类和管理鉴权测试通过。
- 修改前相同用例复现 5 类统计错误：暂停、冷却、默认并发满、独立并发满、历史错误状态。
- 完整 `go test -race -count=1 ./...`、go vet、gofmt、git diff --check、前端静态检查通过。隔离实例上的浏览器设置保存/重载、明暗主题及移动布局检查通过。

性能：仅采集 `/metrics` 时新增一次短锁内的账号池快照，请求转发路径没有新增计数或校验。同机三次 300ms 基准取中位数，包含整个指标生成过程，不包含网络：

| 账号数 | 修改前 | 修改后 | 增量 |
|---|---:|---:|---:|
| 12 | 0.013349 ms | 0.014695 ms | 0.001346 ms |
| 1000 | 0.218373 ms | 0.323725 ms | 0.105352 ms |

这些数据说明常规采集开销很小，不作为任何采集频率下完全零争锁影响的保证。没有为采集指标增加后台轮询或网络请求。

部署：在途请求为零时更新服务，旧运行程序和状态文件备份在 `data/backups/before-metrics-fix-20261029-234616/`。运行二进制 SHA256 为 `87ba34296ea6276343c964b1209ebe1d770d479db7ee661febbc8627f42981f3`。健康检查通过；线上指标 ready=12，与账号池快照一致；账号数量及设置摘要未改变。本次未发送真实模型请求或领取奖励。

证据：[定向测试](evidence/metrics-ready-fix-2026-10-29/targeted.log)、[旧版对照](evidence/metrics-ready-fix-2026-10-29/baseline-tests.log)、[完整竞态回归](evidence/metrics-ready-fix-2026-10-29/race.log)、[性能](evidence/metrics-ready-fix-2026-10-29/benchmark-summary.json)、[运行核对](evidence/metrics-ready-fix-2026-10-29/live.json)、[浏览器检查](evidence/metrics-ready-fix-2026-10-29/browser-smoke.log)。
