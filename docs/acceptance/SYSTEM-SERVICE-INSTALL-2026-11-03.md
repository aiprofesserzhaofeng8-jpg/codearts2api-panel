# 系统级一键安装验收

对应 [实施计划](../architecture/SYSTEM-SERVICE-INSTALL.md)，交付版本 [v0.3.5](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/tag/v0.3.5)。生产代码提交为 `e8fcc8e`；其后 `73cee77` 仅修正已有取消测试的同步时序。

## 交付行为

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash
```

无参数安装最新稳定版到 `/opt/codearts2api`，创建 `codearts2api.service` 和不可登录的专用系统用户；服务开机启动，默认监听 `127.0.0.1:10087`。无需 Go、Node.js 或用户级 linger；需要 Linux amd64/arm64、Python 3.10+、运行中的 systemd、useradd/nologin 及 root。

程序与部署配置由 root 持有，专用用户仅可写 data。初始化凭证位于 `data/state.json.setup.json`，0600；管理员用户名固定 admin，密码由首次向导设置。重复安装拒绝覆盖；已有用户级服务不自动接管。

同一入口加 `upgrade` 读取系统 scope，检查实际进程、版本和 schema，停止后完整备份，再切换程序并验证启动；失败不自动恢复旧凭据或账本。备份需显式 `--system`，见 [部署说明](../../deploy/README.md)。

## 实际验收

- 本地完整 `make check` 通过；52 项部署单元测试、actionlint、shell 语法与文档链接检查通过。
- [Release 工作流](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/runs/41480071063) 全部通过，包含应用与部署回归、两个架构包、Docker/Compose、Caddy/TLS、用户级迁移/升级及故障验收、系统级服务验收、双架构镜像发布。
- 系统级验收在隔离 GitHub Linux 主机以 root 执行，实际安装 `/opt/codearts2api` 和 `/etc/systemd/system/codearts2api.service`；使用真实二进制及 systemd，不触碰真实平台账号。
- 同一 bootstrap 无参数入口完成安装；验证非 root UID、root 所有程序且服务用户不可写、data 可写、初始化凭证权限和所有者、向导及管理员登录、重复安装拒绝、重启、系统级升级、备份生成、密码和状态保留、开机启动，共 11 项通过。
- 升级起点为同代码构建的 `v0.0.0` 隔离基线，目标为正式候选 v0.3.5；发布下载网络在该 root 验收中使用本地夹具。公开 GitHub 下载另行验证，不能把该夹具当作公网网络测试。
- GitHub CI 主机的 `/opt` 初始允许非 root 写入，触发生产路径检查。工作流仅在临时验收主机设置 `/opt` 为 root:root 0755；安装器仍拒绝不安全的父目录。
- 普通 CI 暴露已有取消测试的异步时序问题；修正只等待服务端确认取消后再放行模拟响应。该测试 `go test -race -count=30` 通过，未改生产取消或成长任务逻辑。
- 最终 [main CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/runs/41481370082) 与 [develop CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/runs/41481369040) 全部通过：Linux 测试/race/部署检查与浏览器页面验收，以及 Windows 构建和测试。两者使用 `73cee77`；与 v0.3.5 的生产代码一致，差异仅上述测试修正。

## 公开发布与范围

- v0.3.5 已公开并设为最新稳定版，提供两个架构包、install.sh、panelctl.py、SHA256SUMS 五个附件。
- 公开 main 安装脚本可下载，内容与本地一致，帮助包含系统级参数。通过公开管道执行 check 可发现 v0.3.5，download 下载程序与工具并通过摘要校验；仅下载，不安装到本机。
- 继续从正式 Release 下载 install.sh 与 arm64 包，四个附件的 SHA-256 均匹配；公开 amd64 包实际执行返回 `codearts2api v0.3.5`，arm64 包 ELF 架构正确。独立 panelctl.py 的安装帮助包含 `--system`，与 main 入口匹配。
- 当前共享出口的匿名 GitHub API 查询遇到 403 限流；使用既有 GitHub 认证后上述公开查询/下载成功。认证仅经隔离子进程环境发送到固定 GitHub API，未写入文件或输出。安装命令仍受网络和 GitHub 限流影响，参见部署说明。
- 未将本机现有用户级服务迁为系统级；本机仍运行 v0.3.3、端口 10088，原数据与服务保持使用。未在 green/华为云执行安装，未调用真实推理或领奖接口。
- amd64 实际运行；arm64 交叉构建并打包，未做 ARM 真机系统服务验收。未验证公网 ACME，脚本不开放防火墙或云安全组。
