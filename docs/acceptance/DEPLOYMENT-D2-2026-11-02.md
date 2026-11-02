# D2 专项验收（2026-11-02）

对应 [D2 实施计划](../architecture/DEPLOYMENT-PLAN.md#d2https-与容器生产入口)。D2 已完成实现与本地隔离验收。基线为 `64f4191`，本轮交付保存在工作区，未部署公网或发布。

## 交付与实际修复

- 宿主机 [Caddy 模板](../../deploy/Caddyfile)：HTTPS 入口、Host 保留、伪造转发头清理、API/SSE 不压缩、静态资源压缩，不增加代理重试、响应缓冲或固定生成写超时。
- [生产 Compose](../../deploy/compose.production.yml) 与环境样例：必须指定镜像，默认仅绑定 `127.0.0.1`；单一命名数据卷、UID/GID 10001、权限限制、30 秒停止宽限期和安装兼容健康检查。本轮运行验证确认已有配置可用，无需修改。
- [TLS 验收脚本](../../deploy/tests/d2_proxy_acceptance.py) 加入 Release 验收；容器验收补充实际端口绑定、权限设置及不可达健康失败检查；HTTP 验收补充登录后携 Cookie 读取会话。
- [部署指南](../../deploy/README.md) 补充宿主机 Caddy 的 systemd 环境变量与重启方法，区分 Compose `.env`、应用公开 origin 与 Caddy 配置。

实测确认的缺口：新安装尚未配置 `public_origin` 时，后端通过 HTTP 接收 Caddy 请求，首次 HTTPS 登录的会话 Cookie 缺少 `Secure`。修复前 TLS 验收在该断言失败；修复后通过。

修复仅在 Caddy 中对 HTTPS `/auth/*` 响应补齐 `Secure`，不改变 Cookie 值、路径、HttpOnly、SameSite 或角色。HTTP 本地登录和后续 Cookie 会话仍通过。不修改后端认证、前端或三个推理平台；公开 origin 仍需设置，以生成正确的第三方登录回调地址。

## 已执行验证

环境：Linux amd64、Python 3.12.7、Caddy 2.10.2、Docker 29.0.2、Compose v5.0.2。使用临时状态、合成用户和本地模拟上游；真实上游请求为 **0**。

| 命令 | 结果 |
|---|---|
| `make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` | 通过，包含现有前端、Go 测试/vet、架构/API/页面检查及 32 项部署单元测试。 |
| `python3 deploy/tests/d2_proxy_acceptance.py --archive artifacts/deployment-final/codearts2api-panel-v0.3.94-linux-amd64.tar.gz --caddy /tmp/codearts-deploy-tools/caddy` | **11 组通过**：受信 TLS 首次安装、初次管理员 Cookie、HTTP→HTTPS、SPA/管理员会话、普通用户与角色隔离、跨源拒绝、转发头、错误透传、四类 SSE、取消和静态压缩。 |
| `python3 deploy/tests/docker_acceptance.py --image codearts2api:deployment-acceptance` | **6 组通过**：生产 Compose loopback/非 root、等待安装与未就绪、正常健康、不可达故障、停止后卷导出/备份恢复、重启后原密码登录。 |
| `python3 deploy/tests/live_acceptance.py --archive artifacts/deployment-final/codearts2api-panel-v0.3.94-linux-amd64.tar.gz --caddy /tmp/codearts-deploy-tools/caddy` | **8 组通过**：既有安装/恢复/账本验收以及 HTTP 代理登录会话、SSE、取消和转发头兼容。 |
| Caddy 生产域名配置校验、Compose 配置解析 | 通过；未指定镜像直接拒绝，明确版本镜像不从本地源码构建，单卷与 loopback 配置有效。 |
| actionlint、shell 语法、Python 编译、文档链接及 `git diff --check` | 通过。 |

TLS 测试使用 Caddy 临时本地 CA，Python 客户端显式信任该 CA 并验证服务端证书；未信任客户端必须验证失败。测试不关闭证书验证、不安装系统根证书、不申请公网证书。只为隔离测试覆盖监听地址、端口和禁止安装系统信任，代理路由及响应头逻辑使用工作区原模板。

四个 POST 流式入口 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/v1/search/messages` 实测首段分别为 **43.7、5.2、6.5、5.3ms**。模拟上游首段后等待 0.8 秒发送后续内容，每项断言首段小于 0.6 秒、无响应压缩，正文包含中文、大整数和工具结果的原始字节完整保留。客户端关闭后 3 秒内模拟上游确认取消。该结果是本机代理传输验收，不是实际模型性能或吞吐量基准。

429/500 测试确认状态、JSON 正文和 `Retry-After` 保留，上游仅收到一次请求。静态 CSS 的 gzip 压缩保持有效。原始摘要与当前配置/脚本摘要见 [results.json](evidence/deployment-d2-2026-11-02/results.json)。

## 边界与运行状态

- 未执行公网 DNS、80/443 云安全组或 ACME 签发验收；这些需要实际域名和部署环境。当前完成的是可部署配置与真实本地 TLS 验收。
- 私有 `v0.3.94` 包和测试镜像只提供未修改的应用二进制；Caddy、Compose 与验收脚本从当前工作区加载。旧包内模板尚未重新打包，不代表 D2 已发布。
- 没有修改推理热路径、平台协议、账号调度、凭据或账本格式；新增 Cookie 规则只用于 HTTPS 认证响应。
- 只支持单实例写一个数据目录。CDN/多级代理需明确可信代理配置，不直接信任传入转发头。
- 临时应用、Caddy、Compose 项目及测试卷已清理，系统信任库未改变。现有 `codearts2api-panel.service` 仍为 active、PID 108144（127.0.0.1:10088）；未重启或切换远程服务。

参考：[Caddy 代理与流式语义](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)、[Caddy 响应头修改](https://caddyserver.com/docs/caddyfile/directives/header)、[自动 HTTPS](https://caddyserver.com/docs/automatic-https)、[Compose 服务配置](https://docs.docker.com/reference/compose-file/services/)。
