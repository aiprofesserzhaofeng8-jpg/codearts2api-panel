# 开发约定

## 当前布局与阶段

Go 模块位于 `backend/`，模块名为 `codearts2api`。服务入口为 `backend/cmd/panel`，诊断入口为 `backend/cmd/probe`。当前沿用成熟平台底层实现及嵌入页面，逐批抽取业务边界；独立前端和存储拆分按 `docs/architecture/MIGRATION.md` 后续实施。

M3 第一批已将中立账号迁入 `internal/accounts`，平台依赖装配迁入 `internal/app`。共享代码使用 `accounts.Account`；`codearts.Account` 仅为迁移别名。旧 `session` 槽由 store 兼容编码器读取、CodeArts 解释，不能为新平台继续往该接口堆方法或往中立账号添加平台字段。请求路径不能为了读取旧会话而反复解析 JSON。

M3 第二批将 CodeArts 对话传输包装为 `internal/upstream/codearts`，普通单次执行归 `internal/gateway.Completions`，SSE 聚合及请求流式选项归 `internal/protocol`。适配器借用已选中的不可变凭据；不能另建 JSON 凭据解码路径。gateway 不导入 HTTP、Store 或具体平台，不增加选号、刷新或重试层。该批尚未迁移的摘要、全局重试和其他平台执行按后续批次逐项处理。

M3 第三批将 WorkBuddy/Trae 的单次流式执行及非流式聚合也接入 `gateway.Completions`。`upstream.BindStream` 只借用已解析凭据并传递型号、会话和请求标识；原生协议、登录、搜索、奖励仍归各平台。两平台只实现最小流式能力，不伪造原生 Chat 或空响应回退接口。32MiB 聚合上限保持原值；凭据解析/刷新互斥、提示词恢复、全局重试和摘要编排仍待后续迁移。

M3 第四批将提示词准备/恢复编排迁入 `gateway.RunPromptPolicy`，摘要识别/准备归 `requestpolicy`。server 仅投影平台配置、判定摘要豁免，并通过两个 hook 保留原尝试计量及预算/分组权限复核。顺序必须是结束旧尝试、复核预算/权限、准备替换请求、开始新尝试；收到任何上游分片后不能恢复。摘要的完整性缓冲、备用型号选择、重试、冷却和结算仍在原实现，本批没有扩展其他平台摘要能力或公共提示词继承。

M3 第五批将摘要完整性验证、8MiB 私有缓冲及最多一次截断重试迁入 `gateway.RunSummary`，使用既有 `StreamTransport`，不增加平台能力。server 通过 Prepare/Start/Usage/Result hook 保留备用型号解析、预算与分组权限、尝试计量、用量和模型冷却；准备成功后才开始计量。错误仍为请求级终止，不能经外层换号放大摘要重试。分片仍只解析一次、先计量再检查大小、复制原字节；只有完整正文才交付客户端。全局换号、WorkBuddy 短恢复和凭据业务仍待迁移。

M3 第六批将全局换号、单一总超时、租约生命周期及总尝试次数迁入 `gateway.RunAccountRetry`，同租约的有限恢复顺序归 `gateway.RunRecovery`。server 的 RetryDriver 保留原子准入、当前权限/预算、实际型号上下文及平台健康分类；WorkBuddy 仍独立判定 500/502/503/504、300ms 等待和 3s 进入窗口。短恢复消耗一个总尝试槽，不能重置总超时；窗口只限制进入，不能缩短生成时间。释放租约后才写健康结果；取消/请求级终止优先于平台分类，已提交输出在原有健康处理后禁止重放。准入锁、凭据刷新和账本没有随循环迁移。

M3 第七批将 WorkBuddy/Trae 的凭据解析、刷新互斥及提交协调迁入 `accounts.ProviderCredentials`，app 注入 Store 窄接口和实时续期开关。HTTP 仅选择适配器并单向委托；平台仍解释凭据和执行刷新，Store 仍是唯一条件提交方。正常路径仍只 Normalize 一次，不新增凭据 JSON 编解码或网络调用。`OperationLocks` 保留原有可取消按账号通道锁；凭据、领奖、成长任务使用三个独立实例，不能合并锁域。CodeArts 原生 Manager、IDE 会话读取和 DPoP 保持原实现，凭据表示及条件事务的后续迁移仍待独立验收。

不要为凑齐目标树创建空包、空服务或无法运行的配置。具体职责和依赖见 `docs/architecture/`。

## 命令与验证

- 仓库根：`make check`、`make race`、`make build`、`make docker`；Go 不在 PATH 时通过 `GO=/absolute/path/to/go` 指定。
- Go 原生命令先进入 `backend/`：`go test ./...`、`go vet ./...`、`go build ./cmd/panel`。工具链版本以 `backend/go.mod` 为准。
- 根 `build.sh` 输出 `bin/codearts2api-panel`；`start.sh` 显式使用根 `data/state.json`，与调用者 cwd 无关。构建工具可通过 `CODEARTS_GO_BIN` 指定，版本通过 `CODEARTS_VERSION` 注入。
- 页面静态检查：根目录执行 `python3 tools/check_html_js.py`。启动器：`python3 -m unittest discover -s scripts -p test_connect_client.py`；Pi：`node scripts/pi/codearts-compaction.test.mjs`。
- 真实推理、领奖或共享凭据刷新按用户明确范围执行，常规回归使用隔离状态和假上游。
- 文档修改检查本地链接。历史验收中的原始命令和指标保留其当时上下文，当前运行说明使用新路径。

## 保持业务边界

账号权限复核、选号和并发占位维持同一原子入口，租约幂等释放。纯目录迁移不改变锁、重试次数、协议、请求参数和状态 schema。

凭据提交保留代际校验；已轮转 token 不允许用旧备份覆盖。结算账本和历史价格保持现有恢复保证，不用普通异步日志替代账本。

CodeArts、WorkBuddy、Trae 的平台协议相互独立。协议转换保留工具数据、大整数、合法拒绝和推理字段；不做全局文本清理。

## 运行与提交

修改服务前核对 PID、二进制、cwd、端口和状态参数。构建成功不代表已部署；模拟测试不代表真实上游通过。验收记录写明版本、环境、结果和未覆盖范围。

账号、密钥、refresh_token、访问恢复文件、运行状态和真实测试请求不进入 Git、镜像上下文或公开报告。提交说明沿用 `CONTRIBUTING.md`，第三方来源许可随文件迁移保留。
