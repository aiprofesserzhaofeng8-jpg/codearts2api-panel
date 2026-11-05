# 全局统一模型 U6 综合验收

日期：2026-11-05。范围：[U6 计划](../architecture/UNIFIED-MODELS-PLAN.md#u6综合验收与可安装交付)。状态：U6 已完成；U1–U6 全部完成（隔离验收与本地安装交付）。基线提交为 `6e8a6cdfa182cf697c915fb05a41e46ae6900cee`，U1–U6 为当前未提交工作区修改。

## 交付

- 新增 [交付专项测试](../../backend/internal/server/unified_delivery_test.go)，通过两个真实平台适配器连接本地模拟上游，覆盖三协议、流式/非流式、首段前切换、实际原始型号、单次结算、并发满、真实 HTTP 取消及重试前账号/授权/预算复核。
- 通过本机 **Pi 1.4.0** 和未改动的 [启动器](../../scripts/connect-client.py) 连接隔离网关，使用公开名读取 profile；完成一次短文本及一次实际 read 工具闭环，共 3 次推理请求。临时目录内配置和文件随测试销毁，密钥仅通过进程环境传递。
- 新增 `panelctl.py recover-schema`：默认检查；显式 `--apply` 验证记录指定的版本、路径与状态，停写后向前继续。支持候选程序发布前、发布后、current.next 创建后、旧数据移走后、新数据就位后、current 切换后及 starting 阶段。使用原子不覆盖目录移动；不恢复旧凭据/账本，完成后重跑为无操作。
- [恢复测试](../../deploy/tests/test_schema_recovery.py) 覆盖各阶段、只读检查、无效/重复/链接/不完整布局、失败启动后保留已轮转数据及重试；恢复预检查明确验证 schema 11，不复用只接受 schema 10 输入的升级校验。
- [实际包演练](../../scripts/unified-delivery.py) 解压校验候选包、执行真实二进制与迁移 CLI、升级/重启及 7 个中断布局恢复。只将 systemd 控制替换为临时子进程，迁移、目录切换、认证、HTTP、持久化和真实进程身份校验仍执行。
- 提供 [管理员说明](../GROUP-MODELS.md)、随 Linux 包提供的 [使用/升级/恢复说明](../../deploy/UNIFIED-MODELS.md) 及 Windows 对应边界；更新过时的分组映射编辑方法和当前 schema 说明。

## 目标与证据

| 目标 | 结果 |
| --- | --- |
| G1 同名绑定两个平台 | `TestUnifiedUngroupedAllProtocols`、`TestMappedModelAllProtocolsChoosePhysicalSource` 与 U6 切换测试；原生适配器收到各自准确型号，响应保留公开名 |
| G2 不分组调用 | 模型列表/profile/三协议测试通过；Pi 不指定 --model 时取得全局默认公开名并实际调用 |
| G3 权限不扩大 | 账号范围、固定/all、空集、raw ACL、visible/callable、预算和原子准入测试；并发满与请求取消无泄漏 |
| G4 有界切换 | 三协议共 6 种模式切换后成功，每个逻辑请求只有一笔结算；已有首段后不重放、总次数、取消及修订变化测试通过 |
| G5 一致模型定义 | 列表/profile/三协议、工具大整数/图片保留测试；真实 Pi 工具执行与实际模型计量通过 |
| G6 迁移与诊断 | 迁移幂等、冲突/损坏/未来版本、journal 恰好重放一次、保存失败回滚、CAS；真实包升级、旧二进制拒绝、缺参数诊断及恢复演练通过 |
| G7 可核验交付 | 本地 v0.4.0 候选包；真实跨平台上游未验证，未发布、未推送、未部署到已有服务 |

上述目标复用 U2–U5 已纳入 Go 全套检查的用例，U6 补充端到端交付证据。没有把多个阶段各自的局部测试冒充真实账号验收。

## 检查

已通过 `make check`（52 项前端、99 项部署检查、Go 全套、vet、契约和架构检查）、`make e2e`（26 项页面回归及首次安装真实 HTTP 验收）、U5 真实管理接口浏览器用例、U6/Pi 专项 race、Windows amd64 vet、10 项启动器测试及 Pi 摘要恢复测试。完整 `make race` 通过（server 包 559.268 秒），之后新增的来源型号断言由专项 race 再次验证。

Go 1.27.3，所有 Go 构建/检查均使用 `GOMAXPROCS=2 GOFLAGS=-p=2`，直接 Go 检查前已构建前端。`scripts/connect-client.py` 的 U1 基线 SHA-256 保持 `94ca9a14900792421df6a12f6226b8e8e3a082c0b8dd5370fb3c18c170c11100`。

## 包与升级演练

候选版本 **v0.4.0**、schema **11**，在 `artifacts/unified-models-u6/final-release/` 生成 Linux amd64、Linux arm64 和 Windows amd64 包及各自 SHA-256。所有归档成员、文件摘要及 manifest schema 已检查；Linux amd64 解压后二进制实际执行，arm64/Windows 为交叉构建与包校验，未在对应原生系统运行。

旧程序来自基线 HEAD 的 backend 源码，注入版本 v0.3.18；仅用于 schema 10/11 行为，嵌入本次相同前端资源。旧程序在临时目录生成 schema 10 状态，新候选完成升级并保留 key budget、固定来源授权及定义 ID；重启后相同。旧二进制随后拒绝 schema 11 且未改状态文件。

演练依次构造 7 个切换中断后的磁盘布局，用真实候选程序继续启动并验证模型定义；不是对真实服务实施 SIGKILL。候选发布前恢复使用原包和摘要。完整升级前备份仅恢复到新的隔离目录，未覆盖正在运行的数据。失败启动后的最新凭据保留由部署单元测试确认。

## 热路径与性能

`catalog.ClientModels/Profile/resolveUnifiedMode` 只读取既有 Store/Registry 的配置和目录快照，没有新增上游请求或凭据解析。`UnifiedSnapshot` 复制定义与策略，未复制凭据。`FilterCapabilities` 每请求检查内容类型，不改输入、不按 SSE 帧执行。`AcquireUnifiedAccount` 在原存储锁内复核 revision/授权并占位，仍由原 `RunAccountRetry` 持有单次总期限和次数，未增加另一层重试或逐分片落盘。

[既有准入 benchmark](../../backend/internal/storage/admission_benchmark_test.go) 增加同规模比较：mapped compatibility 与 unified 均使用两个来源、相同组、13/128 个账号、least_inflight、关闭粘性、相同固定时钟，3 轮 `-benchmem`。统一路径多一次来源列表分配（32 B、1 alloc），用于保留当前事务校验后的候选集合；13 账号中位数为 1796→1846 ns/op（+2.78%），128 账号为 12921→13210 ns/op（+2.24%）；具体样本与命令见机器记录。它只度量准入，不代表网络/模型端到端吞吐，也不是相对 U1 前代码的性能结论。

## 边界与交付状态

本轮未确认有两个具备测试额度且型号对应关系已验证的真实来源，未使用真实平台凭据、刷新或领奖。因此**真实跨平台未验证**，模拟上游通过不代表实际平台身份或额度可用。Windows/arm64 原生运行、真实 systemd 安装、公开发布与已有服务部署均是独立状态。

实际运行实例未更新，生产状态和真实客户端配置保持原状。本地验收包不等于 GitHub Release。详细命令、结果、包和源码摘要见 [机器记录](UNIFIED-MODELS-U6-CHECKS-2026-11-05.json)。
