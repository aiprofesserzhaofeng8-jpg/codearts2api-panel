# D1 专项验收（2026-11-02）

对应 [D1 实施计划](../architecture/DEPLOYMENT-PLAN.md#d1一致性备份恢复和部署诊断)。D1 已完成实现与本地隔离验收。基线为 `64f4191`，交付位于当前工作区，尚未提交或公开发布。

## 完成内容

| 能力 | 最终行为 |
|---|---|
| 一致性备份 | 先停止实际受管服务，归档专用目录中的全部普通文件，包括主状态、账本、成长检查点、安装与恢复文件。不可读文件或子目录直接失败；归档前后检查清单，并验证归档中的实际字节。备份失败不留下半成品。 |
| 服务状态恢复 | systemd/Compose 停服后确认没有本机写者；成功和失败路径都恢复原运行状态。原本停止的服务保持停止；Compose 暂停、重启中、未知或缺失实例拒绝操作。 |
| 恢复不覆盖 | 验证清单、摘要、路径及主状态版本，写入临时目录并同步文件和目录，再通过 Linux `RENAME_NOREPLACE` 原子提交。目标即使在校验后被并发创建为空目录或链接也不会覆盖。 |
| 数据恢复 | 恢复到新目录后，原管理员密码和 API 密钥可用。保留尚未合并进主快照的用量账本，使用原结算金额重放一次；再次重启不重复累计。 |
| 只读诊断 | 检查主状态、附属文件、嵌套目录与文件权限、HTTP 健康状态。主状态丢失但附属文件仍在时报告异常；输出不包含凭据、文件内容或上游响应。 |
| 健康检查 | 区分 `running`、`setup_required`、`unhealthy`；等待首次安装视为存活，但不视为可接推理流量。 |

本轮复核修复了首轮实现遗漏的不可读子目录、恢复目录并发覆盖窗口、Compose 非稳定状态，以及不完整状态和嵌套目录权限诊断。

## 验证结果

环境：Linux amd64、Python 3.12.7、用户级 systemd、Docker 29.0.2、Compose v5.0.2。所有数据、密码和账本记录均为临时合成数据，真实上游请求为 **0**。

| 命令 | 结果 |
|---|---|
| `make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` | 通过：现有前端、Go 测试/vet、架构/API/页面检查及工具测试全部通过，包含 32 项部署单元测试。 |
| `python3 -m unittest discover -s deploy/tests -v` | 32 项通过，其中本轮新增 12 项故障和边界测试。覆盖非法归档、摘要篡改、并发恢复、读取/写入失败、服务重启失败、离线写者、状态诊断及脱敏。 |
| `python3 deploy/tests/live_acceptance.py --archive artifacts/deployment-final/codearts2api-panel-v0.3.94-linux-amd64.tar.gz` | 4 个场景通过：真实二进制首次安装/登录、运行写者拒绝、备份恢复后登录/密钥保留、非空账本恢复幂等。 |
| `python3 deploy/tests/managed_acceptance.py --first artifacts/releases/codearts2api-panel-v0.3.93-linux-amd64.tar.gz --second artifacts/deployment-final/codearts2api-panel-v0.3.94-linux-amd64.tar.gz` | 7 个场景通过：真实 systemd 安装/升级及二进制路径核对、归档校验、原密码沿用、受管备份停启、运行写者拒绝、备份失败后恢复服务。 |
| `python3 deploy/tests/d1_compose_acceptance.py --image codearts2api:deployment-acceptance` | 5 个场景通过：真实 Compose 暂停状态拒绝、备份失败恢复、备份停启、原停止实例保持停止、恢复目录挂载后使用原密码登录。 |
| `actionlint`、`sh -n deploy/healthcheck.sh`、Python 编译、文档链接和 `git diff --check` | 通过。Release 工作流已纳入 Compose D1 验收。 |

账本实测先向已停止的临时实例写入一条主快照尚未包含的合法记录：请求数 1、Token 16、历史结算金额 0.125 USD。备份并恢复后启动真实应用，确认三项分别增加一次；第二次启动均保持不变。该测试验证备份完整性和现有恢复逻辑，不涉及真实账户余额或上游扣费。

当前运维脚本直接从工作区加载/执行。上述 `v0.3.93`、`v0.3.94` 私有旧测试包和镜像仅提供未修改的应用程序，用于隔离运行；包内旧脚本不代表本轮 D1 修复已重新打包或发布。可复核的脚本摘要及合成数据验收结果见 [results.json](evidence/deployment-d1-2026-11-02/results.json)。

## 使用与边界

完整命令见 [部署指南](../../deploy/README.md)。备份必须明确指定 `--unit`、`--compose` 或已停止写者的 `--offline`；恢复必须指定不存在的新目录。恢复完成后不会自动替换在线数据或启动服务。

- 运维命令使用 Python 标准库，独立运行，不进入推理热路径；本轮未修改 backend/frontend、账号调度、凭据刷新或账本格式。
- `renameat2(RENAME_NOREPLACE)` 不受支持时拒绝恢复，不降级为可能覆盖目录的行为。仅在 Linux amd64 环境实测，未声称完成 ARM 真机验收。
- `/proc` 检查不能识别其他主机或不可见命名空间的写者；一个专用数据目录只允许一个应用写进程。生产容器卷导出和 UID 权限流程见部署指南；本轮 Compose 故障验收使用宿主当前 UID 的专用临时绑定目录。
- 归档权限为 0600，恢复文件为 0600、目录为 0700；归档不加密，摘要校验不替代可信来源验证。恢复不自动回写旧凭据。
- 未执行远程部署、GitHub 推送或 Release 发布。临时 systemd/Compose 实例已清理；现有 `codearts2api-panel.service` 保持 active，主进程仍为 PID 108144（本机 10088）。
