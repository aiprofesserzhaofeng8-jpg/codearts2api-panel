# 单实例部署与运维验收（2026-11-02）

对应 [实施计划](../architecture/DEPLOYMENT-PLAN.md)。基线提交 `64f4191`；本轮变更在工作区，未推送标签或发布。D1～D4 的代码、配置、文档及本地隔离验收完成。

本文件保留首轮验收快照。随后 D1 补齐了目录读取失败、原子恢复、诊断和 Compose 状态边界；当前 D1 结果见 [D1 专项验收](DEPLOYMENT-D1-2026-11-02.md)，旧测试包中的脚本不代表补齐后的代码。

## 交付

- `deploy/panelctl.py`：备份、恢复、只读诊断、存活/就绪检查、版本包安装与同 schema 升级；使用 Python 标准库。
- `deploy/package_release.py`：Linux amd64/arm64 包、schema/版本清单、文件摘要及项目/WorkBuddy 许可证。
- `deploy/Caddyfile`、`compose.production.yml`、`.env.example`、`healthcheck.sh`：固定镜像生产部署、首次安装兼容健康状态、SSE 反代。
- 根镜像加入健康脚本和许可证，根 Compose 停止宽限期改为 30 秒。
- CI 加入运维测试；Release 工作流按稳定版本标签触发，验证后推送固定版本镜像并生成 Release 草稿。
- [部署指南](../../deploy/README.md) 包含安装、域名、备份、恢复、升级和故障定位。

## 已执行的验证

环境：Linux amd64，用户级 systemd、Docker 29.0.2、Caddy 2.10.2、仓库锁定 Go/前端工具链。Caddy Docker Hub 拉取遇到 EOF，改用官方 Release 二进制并按官方 SHA-512 清单验证；随后完成真实代理运行测试。

| 验证 | 结果与边界 |
|---|---|
| `make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` | 通过，覆盖前端、Go 测试/vet、架构/API/页面检查；后续新增运维测试另行完整重跑 |
| `python3 -m unittest discover -s deploy/tests -v` | 20 项通过：归档往返、文件权限、诊断脱敏/只读、正在运行的写者、符号链接/穿越/重复/超限/篡改归档、并行工具互斥、端口冲突、非法状态、升级失败与 schema/降级拦截 |
| `live_acceptance.py --archive …v0.3.94-linux-amd64.tar.gz --caddy /tmp/codearts-deploy-tools/caddy` | 7 个场景通过：真实二进制初始化和登录、备份恢复重启、真实代理同源登录、SSE 首段与后续分段、取消传播、Host 保留与伪造转发头清理 |
| `managed_acceptance.py --first …v0.3.93… --second …v0.3.94…` | 5 个场景通过：真实 systemd 安装、升级后二进制路径核对、备份校验、密码沿用、受管备份停启 |
| `docker build --build-arg VERSION=v0.3.94 -t codearts2api:deployment-acceptance .` | 通过，使用当前 Dockerfile |
| `docker_acceptance.py --image codearts2api:deployment-acceptance` | 5 个场景通过：生产 Compose、UID 10001、安装等待/就绪/正常健康、停止后复制数据卷并归档恢复、重启后登录 |
| 两架构版本包 | amd64/arm64 构建成功，核对 ELF 架构、schema 10、许可证；amd64 实际运行，arm64 仅交叉构建 |
| Caddy validate、Compose config、shell 语法、actionlint 1.7.7、文档链接及 git diff 检查 | 通过 |

上述 `v0.3.93`、`v0.3.94` 只是隔离验收版本号，不是已发布版本。最终私有测试包在 `artifacts/deployment-final/`（不入库）；已确认包中的运维脚本和指南与工作区一致。可公开的合成数据验收摘要见 [results.json](evidence/deployment-2026-11-02/results.json)。

SSE 测试让模拟上游首段后等待 0.8 秒再发送第二段，断言首段在 0.6 秒内到达且没有响应压缩；取消测试断言客户端关闭后上游在 3 秒内观察到连接关闭。这是代理行为验收，不是模型吞吐率测试。

## 数据和故障语义

- 专用目录完整归档，包含所有普通文件和附属状态；先停止写者，备份前后比对，并二次验证归档内容。归档 0600，恢复文件 0600/目录 0700。
- 工具级文件锁防止同一目录的运维命令重叠；不替代应用锁或管理员停止所有写者的责任。`/proc` 不能证明其他主机/隐藏命名空间没有写共享卷。
- 恢复只接受新目录，禁止覆盖已有凭据。备份未加密，摘要不能替代来源认证。
- 升级只接受递增稳定版本和相同 schema，实际状态再次复核。备份失败恢复原服务；切换后启动失败停止新服务，保留最新数据、旧程序及备份，不自动回写旧凭据。
- 故障测试模拟新程序启动后轮换 token 再失败，确认最新 token 保留、没有数据回滚。
- 安装前端口占用会直接拒绝；安装/升级成功还必须核对 systemd MainPID 指向的实际二进制。

## 未执行与限制

- 未在 GitHub 上执行 Release 工作流，未公开发布镜像或版本包；工作流已通过 actionlint，本地完成其主要构建和验收命令。
- 未验证 ARM 真机运行、真实域名 ACME 签发、云安全组/CDN 配置。代理实测使用本地 HTTP，HTTPS 配置通过解析，不等同于公网证书已签发。
- 不支持无停机升级、多实例共写或自动跨 schema 回滚；备份停机使用应用现有最多 10 秒排空语义。
- 没有发起真实上游推理、领奖或凭据刷新。未修改 backend/frontend 源码、推理处理路径、调度或账本格式。
- 临时 systemd unit、Docker Compose 项目和测试卷已清理；本机 10088 仍由原 PID 108144 服务，远程服务未切换。
