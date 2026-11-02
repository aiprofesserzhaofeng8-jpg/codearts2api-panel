# 部署入口

根 [Dockerfile](../Dockerfile) 和 [docker-compose.yml](../docker-compose.yml) 是镜像与 Compose 的基础入口。本目录只维护补充示例。

## systemd 用户服务

将 [codearts2api-panel.service.example](codearts2api-panel.service.example) 复制到用户 systemd 目录，修改为实际绝对路径并执行 daemon-reload 后启用。示例假定源码位于 `~/codearts2api-panel`，先在根目录运行 `./build.sh`。状态路径由 `start.sh` 显式指定。

升级顺序：备份当前二进制与运行数据、构建新二进制、验证产物、重启、核对 `/health` 和版本。源码目录迁移不改变账号数据格式。

示例只监听本机。公网 HTTPS 与 SSH 隧道继续按 [主 README](../README.md) 配置，具体地址、密码和私钥不写入模板。

M7 单实例多用户入口为 `/user`，管理员在 `/` 创建用户并分配分组和共享预算。仍只启动一个写状态的进程；多个进程不能共写 JSON/账本。HTTPS 在反向代理终止时，需为管理员及 `codearts_user_session` Cookie 添加 Secure 属性。schema 8 升级和受限回退见 [用户指南](../docs/USERS.md)。
