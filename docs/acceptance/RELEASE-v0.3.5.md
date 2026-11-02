# v0.3.5

新增 Linux 系统级一键安装：执行以下命令，即可下载并校验最新稳定版、创建专用系统用户、安装并启用 systemd 服务。

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash
```

默认安装目录 `/opt/codearts2api`，服务名 `codearts2api.service`，监听 `127.0.0.1:10087`。程序以不可登录的 `codearts2api` 用户运行；程序与部署配置由 root 管理，只有数据目录对服务用户可写。使用 SSH 隧道或 HTTPS 反代访问，显式 `--host 0.0.0.0` 可选择直接监听所有网卡。

通过 `sudo cat /opt/codearts2api/data/state.json.setup.json` 读取初始化凭证，在首次安装向导设置 admin 密码。凭证不会自动打印到安装日志；无通用默认密码。

后续升级：

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash -s -- upgrade
```

重复安装拒绝覆盖已有目录、服务或数据。升级沿用完整备份、版本/schema 检查和失败后保留最新数据的策略。原用户级实例继续使用原命令和服务，不自动接管或复制凭据。

需要 Linux amd64/arm64、Python 3.10+、systemd、useradd/nologin 和 sudo/root。应用协议与 schema 10 不变；未增加支付或公开注册。系统级备份需显式 `--system`，详见 [部署指南](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/blob/v0.3.5/deploy/README.md)。
