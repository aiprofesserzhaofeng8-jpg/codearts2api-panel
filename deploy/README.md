# 单实例部署与运维

本页运维工具适用 Linux amd64/arm64。Windows x64 ZIP 的启动、备份和升级见 [Windows 使用说明](windows/README.md)。一个进程写一个专用数据目录；多个进程/容器不能共享写 JSON 状态和账本。Linux 运维工具在宿主机运行，需要 Python 3.10+；恢复还要求 libc、内核和文件系统支持 `renameat2(RENAME_NOREPLACE)`，不支持时拒绝恢复，不降级为可能覆盖目录的操作。不进入推理热路径，不改上游协议。

源码构建仍使用根 `Dockerfile`、`docker-compose.yml`、`build.sh` 和 `start.sh`。`deploy/` 提供生产镜像配置、反代模板、版本安装及离线数据工具。

## 选择部署方式

| 场景 | 入口 |
|---|---|
| Windows x64 本机使用 | Release ZIP 全部解压后运行 `start.cmd`，见 [Windows 指南](windows/README.md) |
| 开发、修改源码 | 根目录 `./build.sh`、`./start.sh` 或 `docker compose up -d --build` |
| Linux 系统级服务 | `curl …/main/deploy/install.sh \| sudo bash`，专用非 root 用户运行 |
| Linux 单机，不安装编译环境 | 固定版本包 + `panelctl.py install`，用户级 systemd |
| Linux Docker 生产部署 | `compose.production.yml` + 明确版本镜像 |
| 公网访问 | 宿主机 Caddy → loopback 后端，使用本目录 `Caddyfile` |

不提供多副本、自动跨 schema 降级、无停机升级或公开注册。

## 一键安装系统级服务

从 v0.3.5 起提供系统级安装。需要 Linux amd64/arm64、Python 3.10+、正在运行的 systemd、useradd/nologin 和 sudo/root。脚本缺依赖时给出提示，不自动升级系统 Python。

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash
```

无参数表示全新系统级安装：`/opt/codearts2api`、`codearts2api.service`、专用系统用户 `codearts2api`、端口 10087、loopback。程序和 installation.json 由 root 管理；data 仅服务用户可写，backups 仅 root 可访问。注册 multi-user.target，退出 SSH 后继续运行，不依赖用户级 linger。

已有同名服务/目录、端口占用、非 root 执行或校验错误均拒绝覆盖。无参数重复执行不会隐式升级。原用户级服务不自动改为系统服务，也不自动复制其凭据。

需要明确版本、路径、端口或所有网卡时：

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash -s -- install --system --version v0.3.5 --root /opt/codearts2api --port 10087 --host 0.0.0.0
```

`0.0.0.0` 需要另外设置云安全组、防火墙及适当的 HTTPS 入口；脚本不修改这些配置。默认 loopback 可以通过 SSH 隧道或本指南中的 Caddy 模板访问。

```bash
sudo systemctl status codearts2api
sudo journalctl -u codearts2api -n 50 --no-pager
# 打开面板完成向导；v0.3.7 仍按旧向导读取初始化凭证
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash -s -- upgrade
```

在向导中设置 admin 密码。凭证内容不自动打印到安装日志，安装后凭证文件删除。升级自动读取 installation.json 的 scope，继续使用同一系统服务和完整备份策略。用户级实例仍由原用户运行原命令。

系统级备份必须显式选择 scope：

```bash
sudo python3 /opt/codearts2api/current/panelctl.py backup --system --unit codearts2api.service --data-dir /opt/codearts2api/data --archive /root/private-backups/panel-日期.tar.gz
sudo python3 /opt/codearts2api/current/panelctl.py doctor --data-dir /opt/codearts2api/data --url http://127.0.0.1:10087
```

恢复后的文件默认归操作用户所有；系统服务恢复到新目录后，在启动前将 data 设为服务用户所有、目录 0700/文件 0600，并保持程序和父目录由 root 管理。恢复不会自动改写 unit 或覆盖现有 data。跨 schema 降级仍不支持。

## 固定版本包与首次安装

从项目的同一个 GitHub Release 下载对应架构的 `codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz`（ARM 用 arm64）、`panelctl.py` 和 `SHA256SUMS`。安装工具作为独立附件提供，其摘要也在清单中；先校验再执行。版本号只是示例，必须替换成实际已发布版本。SHA-256 用于完整性校验，不是发布者签名，文件及校验值都应来自可信发布页。

在下载目录核对：

```bash
sha256sum --check --ignore-missing SHA256SUMS
python3 panelctl.py install \
  --archive codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz \
  --sha256 从校验清单复制的64位摘要 \
  --root "$HOME/.local/share/codearts2api-managed" \
  --name managed --port 10087
```

源码仓库中对应命令为 `python3 deploy/panelctl.py ...`。安装目录不要包含空格、中文、`%` 或 shell 特殊字符。工具不会覆盖已有目录或同名服务。生成 `codearts2api-managed.service`，绑定 `127.0.0.1`；版本保存在 `releases/`，`current` 指向正在使用的版本，`data/` 保存运行数据。

若机器没有用户 systemd 会话，先配置登录会话；需要退出 SSH 后继续运行、重启后自动启动时，由管理员执行 `loginctl enable-linger 用户名`。`--no-start` 仅生成安装和 unit，可随后 `systemctl --user daemon-reload`、`systemctl --user enable --now codearts2api-managed.service`。

当前源码的新安装直接填写管理员邮箱和密码，无需 Token 或验证码。v0.3.7 发布包仍使用旧版 Token 向导。请在对外开放前完成初始化；已安装实例的向导永久关闭，旧版 admin 登录继续兼容。普通用户由管理员创建。

已有源码服务可继续使用 `codearts2api-panel.service.example`，但不要同时启动两套程序写同一目录。工具安装的版本化目录和旧源码目录是不同管理方式，`upgrade` 不会接管任意既有服务。

## Docker 生产配置

在 `deploy/` 或保存生产模板的独立目录执行：

```bash
cp .env.example .env
# 编辑 CODEARTS_IMAGE 为已发布的 ghcr.io/仓库所有者/仓库名:vX.Y.Z 或 digest
chmod 600 .env
docker compose --env-file .env -f compose.production.yml config --quiet
docker compose --env-file .env -f compose.production.yml pull
docker compose --env-file .env -f compose.production.yml up -d
```

不要使用 `latest`。生产模板不会从工作区源码构建；GHCR 包首次发布后的可见性也需在 GitHub 核实。镜像使用 UID/GID 10001 和命名数据卷。改为 bind mount 时，先为专用目录设置该用户可写权限，不要用 `chmod 777`。

打开面板完成首次安装；当前源码无需手动读取初始化文件。

容器健康检查有三种输出：`running`、`setup_required`、`unhealthy`。等待安装时属于存活；`sh /usr/local/bin/codearts-healthcheck --ready` 才是接收推理流量的就绪检查。Compose 的 `unhealthy` 标记本身不会触发 `restart: unless-stopped` 重启，不应误认为已实现自动修复。

根 Compose 的宿主端口仍为 10087，生产模板可用 `CODEARTS_PORT` 调整。两者不能同时占用同一端口，不要让其他项目连接错实例。

## HTTPS 和流式代理

`Caddyfile` 用于**宿主机安装的 Caddy**，不是容器里访问宿主 loopback。设置 Caddy 服务环境变量：

```text
PANEL_DOMAIN=panel.example.com
PANEL_UPSTREAM=127.0.0.1:10087
```

域名指向服务器，安全组放行 80/443；后端端口继续只绑定 loopback。先执行 `caddy validate --config /实际路径/Caddyfile --adapter caddyfile`，再通过 Caddy 的服务管理器重启。模板关闭 Caddy 管理接口，所以不使用 `caddy reload`。域名证书申请需要公网 DNS 和 ACME 连通性，本地测试不代表公网签发成功。

如果使用发行版安装的 systemd Caddy 服务，将模板保存到该服务实际读取的配置路径（通常为 `/etc/caddy/Caddyfile`），执行 `sudo systemctl edit caddy` 添加：

```ini
[Service]
Environment="PANEL_DOMAIN=panel.example.com"
Environment="PANEL_UPSTREAM=127.0.0.1:10087"
```

在相同环境下校验配置后执行 `sudo systemctl daemon-reload`、`sudo systemctl restart caddy`，再检查 `sudo systemctl status caddy`。只编辑 Compose 的 `.env` 不会给宿主机 Caddy 服务设置这些变量。Caddy 的证书数据目录需要持久保存并保持原权限。

在面板「用户权益 → 登录与邮件」将公开 origin 设置为实际 HTTPS 地址，例如 `https://panel.example.com`。这与登录 Cookie 和 OAuth 回调一致性相关，不能单靠伪造 `X-Forwarded-Proto` 替代应用配置。

新安装尚未设置公开 origin 时，后端通过 HTTP 接收代理请求；模板会仅在 HTTPS 的 `/auth/*` 响应上补齐 Cookie 的 `Secure` 属性，覆盖第一次管理员登录。该规则不更改 Cookie 值、路径或角色；HTTP 本地验收仍可使用原有登录会话。公开 origin 仍需配置，供应用生成正确的第三方登录回调地址。

模板保留 Host、覆盖客户端伪造的转发头；默认直接接收客户端连接。若前面再加 CDN，需单独设置可信代理范围，不能直接相信任意 `CF-Connecting-IP`。

仅压缩静态资源；不压缩 API/SSE，不设响应缓冲或固定流式总时限，不增加代理层重试。Caddy 对 `text/event-stream` 自带及时刷出；不要随意设置 `flush_interval -1`，其取消行为可能使客户端断开后上游继续工作。首次安装不依赖 `/health` 通过后才转发，否则用户无法进入向导。

## 一致性备份

备份专用数据目录的**全部文件**，而非仅主状态。包括 `.usage.jsonl`、`.growth.json`、`.installed`、`.setup.json`、`.access.json` 和 `.admin-recovery.json` 等。不可读的子目录或文件会使备份失败，不会跳过后生成残缺备份。目录不能混入其他项目、服务日志、套娃备份或符号链接。默认上限 8 GiB / 100000 个文件；超过时应单独规划存储备份，不随意删账本。

源码或受管 systemd 服务：

```bash
python3 deploy/panelctl.py backup \
  --unit codearts2api-panel.service \
  --data-dir "$HOME/codearts2api-panel/data" \
  --archive "$HOME/private-backups/panel-日期.tar.gz"
```

工具会停止指定服务，检查本机进程后备份，然后恢复原运行状态；失败也尝试恢复。原本停止的实例保持停止；Compose 的重启中、暂停、未知状态以及不存在的实例会直接拒绝，要求先排查。必须指定真正管理该目录的服务，不能另有守护程序或其他主机同时写该目录。备份会短暂中断服务；应用现有排空最多 10 秒，不承诺无限等待长请求。繁忙时应安排维护窗口。

纯离线副本可使用 `--offline` 替代 `--unit`，但必须先停止实际写者及自动重启管理器。本机 `/proc` 检查是辅助防误操作，无法证明其他主机或受权限隐藏进程没有写共享卷。

Docker 命名卷推荐**停止后导出到私有目录再打包**，不依赖宿主 Python 读取 root 拥有的卷：

```bash
# 在生产 compose 所在目录，先准备新的、空的私有暂存目录
mkdir -m 700 /安全路径/panel-staging
docker compose -f compose.production.yml stop -t 30 codearts2api
docker compose -f compose.production.yml cp codearts2api:/data/. /安全路径/panel-staging/
# 确认 cp 成功后恢复原来运行的服务
docker compose -f compose.production.yml start codearts2api
python3 panelctl.py backup --offline \
  --data-dir /安全路径/panel-staging --state-name codearts2api-data.json \
  --archive /安全路径/backups/panel-日期.tar.gz
```

如果由宿主管理员直接读取卷 mountpoint，可用 `backup --compose /绝对路径/compose.production.yml --data-dir 卷目录 --state-name codearts2api-data.json` 自动停启；须保证与原部署使用同一 Compose 项目和环境，且只管理指定服务。

备份为 0600，清单记录每个文件的大小和 SHA-256，但**没有加密**。异机保存时使用受控加密存储，不能上传公开附件。不要使用 `docker compose down -v` 清理部署，它会删除数据卷。

## 恢复和迁移

```bash
python3 deploy/panelctl.py restore \
  --archive /安全路径/backups/panel-日期.tar.gz \
  --target /安全路径/panel-restored
```

目标必须不存在，恢复前完整检查归档路径、普通文件类型、清单、摘要和主状态版本一致性。先同步暂存文件及目录，再通过内核原子提交；即使最后一刻有其他进程创建同名空目录或符号链接，也不会覆盖。不会启动服务。确认备份是最新版本、账本和状态属于同一次停机快照、原服务已经停止后，再将**兼容版本的程序**指向恢复目录。

Docker 恢复使用新的命名卷与单独 Compose 项目：先 `create` 容器（不要启动），再 `cp 恢复目录/. codearts2api:/data/`，用一次性 root 容器将卷内容所有者改为 `10001:10001`、目录 0700、文件 0600，最后启动。禁止边运行边覆盖卷。不要继续启动原实例共用同一上游账号执行后台任务。

历史备份可能含已失效的 refresh token；存在更新的运行状态时，不应让历史备份取代它。恢复是灾难恢复或迁移操作，不是升级失败后的自动回滚策略。

## 升级

工具安装的 systemd 实例：

```bash
python3 deploy/panelctl.py upgrade \
  --root "$HOME/.local/share/codearts2api-managed" \
  --archive codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz \
  --sha256 从校验清单复制的64位摘要
```

顺序：验证包/架构/版本、切换残留及实际运行程序 → 停止服务 → 核对实际 schema → 完整备份 → 切换 `current` → 启动 → 健康和实际可执行文件核对。只允许稳定版本递增，且包与现有状态 schema 相同。跨 schema 迁移需要单独验收，本工具拒绝自动处理，不修改状态版本号。

新程序启动失败时停止该程序，保留旧程序、备份以及**最新数据**，供定位和向前修复；不会自动恢复旧状态。服务切换前的校验/备份失败，会尝试恢复原服务。程序回退也要明确核实 schema、权限语义和当前凭据兼容，不直接覆盖数据目录。

停服务命令失败也会尝试恢复原服务；切换后的目录同步失败按已切换处理，保持候选服务停止。若 `current.next` 或候选版本目录已存在，工具会在停机前拒绝；保留这些现场并先核对上次结果，不盲目删除后重试。

Docker：先按上述步骤备份；核对候选版本的状态兼容说明；将 `.env` 的镜像改成明确新版本，`pull` 后 `up -d`，检查健康与 `docker compose exec codearts2api codearts2api -version`。保留旧镜像。跨 schema 的镜像降级同样禁止盲目执行。

## 排障与检查

```bash
python3 deploy/panelctl.py doctor --data-dir /实际数据目录 --url http://127.0.0.1:10087
python3 deploy/panelctl.py health --url http://127.0.0.1:10087 --ready
systemctl --user status codearts2api-managed.service
journalctl --user -u codearts2api-managed.service -n 50 --no-pager
ss -ltnp 'sport = :10087'
```

`doctor` 只报告服务状态、schema 和文件检查问题，不输出账号或密钥。检查范围包含子目录权限、不可读目录、主状态丢失后遗留的安装/账本等附属文件；诊断不会修改数据。日志属于私有运维数据，不要未经检查直接分享。

| 症状 | 排查 |
|---|---|
| `setup_required` | 服务已存活，读取服务器安装凭证完成向导 |
| 端口冲突 | 用 `ss` 确认实际 PID，核对启动路径；不要直接终止不明进程 |
| 数据不可写 | 检查实际服务用户和卷所有者；不要扩大为全员写权限 |
| 代理登录失败 | 检查保留的 Host、公开 origin、浏览器实际域名和 Cookie |
| 流式卡到最后一次出现 | 检查是否增加 API 压缩/缓存/缓冲，运行代理 SSE 验收 |
| 恢复后拒绝启动 | 检查主状态与附属文件是否完整、程序是否支持 schema；不要删除安装标记绕过 |

## 开发与发布验收

```bash
python3 -m unittest discover -s deploy/tests -v
make check
make frontend
python3 deploy/package_release.py --version vX.Y.Z --arch amd64
python3 deploy/package_release.py --version vX.Y.Z --arch arm64
python3 deploy/tests/live_acceptance.py --archive artifacts/releases/codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz --caddy /路径/caddy
python3 deploy/tests/d2_proxy_acceptance.py --archive artifacts/releases/codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz --caddy /路径/caddy
python3 deploy/tests/docker_acceptance.py --image 本地构建的测试镜像
python3 deploy/tests/d1_compose_acceptance.py --image 本地构建的测试镜像
# 仅在具有用户 systemd 的 Linux 主机运行；创建独立临时 unit，结束后清理
python3 deploy/tests/managed_acceptance.py --first 旧测试版本包.tar.gz --second 新测试版本包.tar.gz --failure-cases
```

发布包构建需要当前提交的前端资源。工作流会先运行完整检查，再构建两个架构；执行 amd64 隔离安装/备份恢复、Caddy 流式和生产 Compose 验收，随后发布固定版本 GHCR 镜像、创建 GitHub Release **草稿**。维护者检查后发布草稿；不会创建 `latest`。推送 `vX.Y.Z` 标签才触发，本地编辑或测试不会发布。工作流不代表 ARM 真机、云安全组或公网证书已验收。

打包先在临时目录完成归档与 SHA-256，再以不覆盖方式发布文件；写入中断不会留下半成品归档，已有包或摘要会拒绝覆盖。两个文件不能保证跨文件原子提交；断电后若只有包没有摘要，需要重新核验来源和内容，不能当作完整发布。正式附件包含两个架构的版本包、独立 `install.sh`、`panelctl.py` 与覆盖四个附件的 `SHA256SUMS`。

`d2_proxy_acceptance.py` 使用临时本地 CA 校验真实 TLS，不安装系统信任、不使用真实平台账号，也不访问公网 ACME。覆盖通过代理安装和登录、角色隔离、HTTP 重定向、转发头、四个 SSE 路径、断连取消、错误透传和静态压缩。容器验收覆盖等待安装、正常运行及不可达故障健康状态。

参考：[Sub2API 部署说明](https://github.com/Wei-Shaw/sub2api/blob/main/deploy/README.md)、[Caddy 流式与取消语义](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)、[Caddy 响应头修改](https://caddyserver.com/docs/caddyfile/directives/header)、[Docker Compose 健康检查](https://docs.docker.com/reference/compose-file/services/#healthcheck)。这里的工具和配置按本项目存储规则独立实现，未复制其业务代码。

## 在线安装入口与更新检查

`install.sh` 从本项目 GitHub 正式 Release 读取版本和附件，校验程序包及 `panelctl.py` 后再执行。需要 Linux amd64/arm64、Python 3.10+、可用的用户级 systemd。先从可信的发布页下载并查看 `install.sh`，然后运行：

```bash
sh install.sh check
sh install.sh download --version v0.3.3 --output ./release-v0.3.3
sh install.sh install --version v0.3.3 --root "$HOME/codearts2api-managed" --port 10087
sh install.sh upgrade --root "$HOME/codearts2api-managed"
```

版本号应换成实际已发布版本；`latest` 默认仅选稳定版。缺附件、网络错误和校验失败不会触发停机。摘要校验验证完整性，下载入口与校验清单仍须来自可信仓库。离线使用前文 `panelctl.py` 命令。容器部署使用固定镜像标签更新，不使用 systemd 安装命令。

管理员在“配置 → 版本与更新”查看真实运行版本，点击检查更新才访问 GitHub。结果缓存一分钟。开发版本不自动比较；可用更新表示存在新版本及当前架构附件，实际 schema 兼容性仍由部署工具在停机前检查。网页不执行升级。

## 已有源码服务迁入受管目录

仅支持当前用户的 systemd 服务、显式 `-data …/state.json`、loopback、相同端口与 schema。源实例有额外环境覆盖或不支持的启动参数时拒绝猜测，应手工核对配置再迁移。先通过服务管理器获取真实二进制与数据目录，使用已校验的发布工具执行：

```bash
python3 panelctl.py migrate \
  --archive codearts2api-panel-v0.3.3-linux-amd64.tar.gz --sha256 '<实际摘要>' \
  --root "$HOME/codearts2api-managed" --name managed --port 10088 \
  --source-unit codearts2api-panel.service \
  --source-binary /绝对路径/codearts2api-panel/bin/codearts2api-panel \
  --data-dir /绝对路径/codearts2api-panel/data --no-refresh
```

`--no-refresh` 仅在旧进程包含 `-no-refresh` 时使用，工具要求保持一致。工具先校验候选，再停止旧进程、备份整个数据目录并恢复至新目录，启动和核对新服务，禁用旧服务。迁移成功后旧目录属于历史副本，**不能重新启动旧服务**，以免旧凭据和账本形成分叉。新服务开始启动后若验收失败，工具停止新服务并保留新数据，需检查日志恢复新实例，不自动启动旧副本。源数据、完整备份、候选目录均保留用于诊断。

新实例管理命令：

```bash
systemctl --user status codearts2api-managed.service
journalctl --user -u codearts2api-managed.service -n 100
python3 panelctl.py doctor --data-dir "$HOME/codearts2api-managed/data" --url http://127.0.0.1:10088
```

GitHub 匿名 API 有查询限额。若显示“拒绝或限制请求”，可稍后重试，或由运维在服务器进程环境设置 `GITHUB_TOKEN`。只查询公开仓库，不需要写入权限；该值仅发送到固定的 GitHub API，不发送到附件下载主机，也不会返回网页。环境凭据不得写入仓库或公开日志。
