# 开发约定

## 当前布局与阶段

Go 模块位于 `backend/`，模块名为 `codearts2api`。服务入口为 `backend/cmd/panel`，诊断入口为 `backend/cmd/probe`。本阶段整体保留原有 `backend/internal` 包边界及嵌入页面；独立前端、业务模块拆分和存储拆分按 `docs/architecture/MIGRATION.md` 后续实施。

M3 第一批已将中立账号迁入 `internal/accounts`，平台依赖装配迁入 `internal/app`。共享代码使用 `accounts.Account`；`codearts.Account` 仅为迁移别名。旧 `session` 槽由 store 兼容编码器读取、CodeArts 解释，不能为新平台继续往该接口堆方法或往中立账号添加平台字段。请求路径不能为了读取旧会话而反复解析 JSON。

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
