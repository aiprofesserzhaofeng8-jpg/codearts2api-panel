# 系统级一键安装

状态：已完成，随 [v0.3.5](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/tag/v0.3.5) 发布。承接现有安装与更新交付，不修改应用、平台协议或状态 schema。见 [验收记录](../acceptance/SYSTEM-SERVICE-INSTALL-2026-11-03.md)。

## 行为

`curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash` 无参数安装最新稳定版本。默认 `/opt/codearts2api`、`codearts2api.service`、端口 10087、loopback；创建专用不可登录的系统用户，应用以该用户运行。显式 `--host 0.0.0.0` 可选择所有网卡。原用户级显式命令保持兼容。

## 实现与验收

1. 安装入口补齐无参数默认值、root/依赖预检和系统级工具能力检查；仍只执行摘要验证后的同版工具。
2. panelctl 复用安装、备份和升级，记录服务 scope；系统级服务使用专用用户、root 所有的程序与配置、仅服务用户可写的数据目录。备份显式选择系统 scope；原用户级数据不自动接管。
3. 同名服务、已有目录、端口占用、摘要损坏、权限不足在覆盖前拒绝；重复安装不隐式升级。不新增自动降级或历史数据回写。
4. 单元测试覆盖 scope、路径权限、默认参数和失败前置；具备 root 的隔离 CI 主机执行真实系统服务安装、初始化、登录、重启、备份和升级，验证服务 UID 与程序/数据权限。保留用户级回归。
5. 更新中英文 README 和部署说明，发布包含新工具的稳定版本后，验证公开管道入口。

系统服务首次安装不自动放行防火墙或配置域名；初次凭证仅存在于私有文件，通过 sudo 在服务器读取。脚本不依赖管道标准输入进行交互。不安装/升级系统 Python；缺少 Python 3.10+ 或 systemd 时给出具体提示后退出。
