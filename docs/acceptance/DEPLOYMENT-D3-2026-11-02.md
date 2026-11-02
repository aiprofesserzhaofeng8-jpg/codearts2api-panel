# D3 专项验收（2026-11-02）

对应 [D3 实施计划](../architecture/DEPLOYMENT-PLAN.md#d3版本包安装和升级)。版本包、用户级 systemd 安装、同 schema 升级和发布工作流已完成实现与本地隔离验收。基线为 `64f4191`，修改保存在工作区，未提交、推送或公开发布。

## 本轮修复与交付

| 项目 | 最终行为 |
|---|---|
| 完整版本包 | Linux amd64/arm64 包包含二进制、版本/schema/架构清单、部署工具、当前 Caddy/Compose 模板、指南和两份许可证，逐文件记录 SHA-256。 |
| 打包失败处理 | 在临时目录完成构建、归档与摘要，再发布完整文件。归档写入失败会清理半成品；已有包或摘要拒绝覆盖，程序化调用同样验证架构。 |
| 安装入口 | Release 增加独立 `panelctl.py` 附件，`SHA256SUMS` 同时覆盖两个架构包和该工具，避免安装前无工具可用。首次安装仍走向导，无默认密码。 |
| 升级预检 | 在停机前检查摘要、平台、版本递增、schema、残留切换文件及实际运行二进制。拒绝降级和跨 schema 自动切换。 |
| 升级失败处理 | 停机前检查失败不重启原进程；停止命令异常也尝试恢复原服务。切换完成后立即进入“已切换”状态，目录同步或启动检查失败时停止候选，保留最新数据和备份。 |
| 自动发布范围 | 标签触发检查、双架构打包、容器构建与本地隔离验收，然后推送固定版本镜像、创建 Release 草稿。未执行远程工作流，未创建公开发布。 |

新增 8 项部署测试。修复前其中 7 项失败，复现了：摘要被覆盖、非法架构未拦截、失败打包残留、遗留切换文件导致不必要停机、未在停机前核对实际进程、停止命令异常后未恢复，以及切换后同步失败时误启动候选。修复后全部通过。

## 已执行验证

环境：Linux amd64、Python 3.12.7、用户级 systemd、仓库锁定 Go/前端工具链、Caddy 2.10.2。全部使用专用临时目录和合成数据，真实上游请求为 **0**。

| 验证 | 结果 |
|---|---|
| `make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` | 通过：前端、Go 测试/vet、架构/API/页面及工具检查，包含部署测试。 |
| `python3 -m unittest discover -s deploy/tests -v` | **40 项通过**，包括新增 8 项 D3 边界测试。最终改动后再次通过。 |
| 当前源码 `package_release.py --version v0.3.96 --arch amd64/arm64 --output artifacts/deployment-d3-final` | 两架构构建成功；ELF 分别为 x86-64/AArch64，版本 `v0.3.96`、schema 10；模板、工具、指南与许可证均与当前工作区逐字节一致。amd64 实际执行版本检查；arm64 未执行。 |
| 发布附件与 `sha256sum --check SHA256SUMS` | 两个架构包与独立 `panelctl.py` 共 3 个条目全部通过；独立工具可正常执行 `--help`。 |
| `managed_acceptance.py --first artifacts/deployment-d3/codearts2api-panel-v0.3.95-linux-amd64.tar.gz --second artifacts/deployment-d3-final/codearts2api-panel-v0.3.96-linux-amd64.tar.gz --failure-cases` | **9 组通过**。正常安装、升级和受管备份使用新包内 CLI，真实创建独立 systemd unit，并核对 MainPID 指向目标二进制。 |
| `live_acceptance.py --archive artifacts/deployment-d3-final/codearts2api-panel-v0.3.96-linux-amd64.tar.gz --caddy /tmp/codearts-deploy-tools/caddy` | **8 组通过**：实际初始化/登录、写者拒绝、恢复与账本幂等、HTTP 代理登录会话、SSE、取消、转发头。 |
| `d2_proxy_acceptance.py --archive artifacts/deployment-d3-final/codearts2api-panel-v0.3.96-linux-amd64.tar.gz --caddy /tmp/codearts-deploy-tools/caddy` | **11 组通过**，包含新包的 TLS 首次安装、角色登录、初次 Cookie Secure、四类流式入口和取消。 |
| actionlint、Python 编译、文档链接和 `git diff --check` | 通过。 |

真实升级故障测试先完成 `v0.3.95 → v0.3.96`，确认管理员原密码可登录，旧程序和完整备份保留。随后测试降级和错误摘要，确认原 MainPID 未变化。

最后构造一份仅用于临时实例的受信合成候选：可以报告版本，启动后写入模拟最新凭据文件和一条有效用量账本，但不提供 HTTP 就绪响应。工具经过实际启动检查失败后停止候选，保留新文件、账本、旧程序和升级前备份，没有用备份覆盖新数据。该测试不访问平台，也不声称模拟了真实 OAuth token 轮转协议。

当前私有交付物位于 `artifacts/deployment-d3-final/`，其中 `v0.3.95`、`v0.3.96` 仅为隔离测试版本号。摘要和源码文件指纹见 [results.json](evidence/deployment-d3-2026-11-02/results.json)。

## 使用与限制

- 使用说明见 [部署指南](../../deploy/README.md)。从可信的同一 Release 获取版本包、独立工具和校验清单，先校验，再安装或升级。
- 已重新打包 D1/D2/D3 的当前部署工具与模板；这不同于首轮旧测试包，仍不等于已在 GitHub 发布。
- 两个输出文件无法跨文件原子提交；断电可能只留下完整包而无摘要，这不视为完整发布。摘要校验保证完整性，不替代可信来源认证。
- 未在 ARM 真机运行，未在 GitHub 执行 Release 工作流，未在本轮实际运行双架构镜像构建与推送；相应配置经过 actionlint，应用二进制已本地双架构构建。D2 的容器运行结果见其专项记录。
- 无自动跨 schema 迁移、无自动数据回滚、无多进程共写或无停机承诺。安装/升级只在维护操作时运行，不进入推理热路径。
- 未修改 backend/frontend、平台协议、账号调度、凭据格式或账本格式。临时 systemd unit、应用和代理均已清理；现有 `codearts2api-panel.service` 仍 active、PID 108144（127.0.0.1:10088），未重启或切换远程服务。
