<div align="center">

# CodeArts2API Panel

**码道 / WorkBuddy / Trae 多账号 API 网关与管理面板**

将码道、WorkBuddy 和 Trae CN/SOLO 账号接入 Chat Completions、Responses 和 Anthropic Messages 兼容接口。

**中文** · [English](README_EN.md)

[![CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?logo=go)](backend/go.mod)

[快速部署](#部署) · [客户端接入](#客户端接入) · [使用文档](#文档) · [提交记录](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/commits/main/) · [贡献者](#贡献者与致谢)

</div>

> 当前版本支持码道、WorkBuddy 和 Trae CN/SOLO；三平台已有真实账号调用验收，Trae 国际版尚未接入。原码道基线保留在 [v0.1.0](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/tag/v0.1.0)。接入见 [多上游说明](docs/MULTI-PROVIDER.md)，实测范围见 [三平台验收记录](docs/acceptance/PARALLEL-MAINTENANCE-2026-10-30.md)。

源码版新增 [Qoder 国内/国际适配](docs/QODER.md)：浏览器授权、续期、动态模型目录、统一模型映射与三个客户端接口。已通过隔离回归，真实账号尚未验收；积分查询、每日奖励与独立搜索尚未接入。

源码版新增 [ChatGPT / Codex 与 OpenAI API](docs/CHATGPT.md)、[Claude / Anthropic](docs/CLAUDE.md) 原生反代：浏览器 OAuth、官方 API Key 导入、自动续期、动态模型、统一映射及三个外部接口。隔离回归已通过，真实上游尚未验收。

源码版另增加 [Kiro 适配](docs/KIRO.md)：Builder ID 设备授权、API Key / OAuth 导入、自动续期、动态模型与现有统一映射。真实上游尚未验收；参数兼容边界见接入说明。

## 项目简介

CodeArts2API Panel 为个人自用提供多平台账号管理、模型请求转发、协议转换与用量统计。服务使用 Go 标准库开发，管理页面嵌入二进制，状态保存在本地 JSON 文件中。

支持在本机、服务器或 Docker 中运行，可设置监听地址与端口，也可通过 HTTPS 反向代理提供公网访问。当前采用单进程、一个管理员管理多个普通用户的架构，不支持多个实例共享同一状态文件。

界面支持中文和英文，右上角可选择“跟随系统 / 中文 / English”。手动选择会保存在当前浏览器；未选择时跟随浏览器语言，无法识别则默认中文。切换会刷新页面，请先保存表单草稿。

前端工程化 M4 已完成：独立 Vue/Vite 构建、页面组件、统一 API 客户端和契约类型；生产仍为单 Go 二进制。开发命令及所有权规则见 [前端说明](frontend/README.md)，验收见 [M4 报告](docs/acceptance/STRUCTURE-M4-COMPLETE-2026-11-02.md)。

运行与存储 M5 已完成：准入协调、纯调度和用量计算已分离，storage 保留唯一事务与账本（M5 当时保持 schema 6）。验证和限制见 [M5 报告](docs/acceptance/STRUCTURE-M5-COMPLETE-2026-11-02.md)。

产品体验 M6 已完成：显式映射预设、分组默认与客户端推荐、独立显示/调用规则和公共提示词逐项继承。使用见 [分组模型](docs/GROUP-MODELS.md) 与 [提示词配置](docs/PROMPT-POLICY.md)，迁移至 schema 7，验证与回退边界见 [M6 报告](docs/acceptance/STRUCTURE-M6-COMPLETE-2026-11-02.md)。

M7 单实例多用户已完成：管理员分配分组和跨密钥共享预算，普通用户通过 `/user` 管理自己的密钥与用量。使用见 [用户指南](docs/USERS.md)，schema 8 和验证范围见 [M7 报告](docs/acceptance/STRUCTURE-M7-COMPLETE-2026-11-02.md)。

用户中心第一批新增跨密钥用户并发上限、昵称/头像和用量仪表盘。本批当时使用 schema 9，旧用户保持原有并发行为。见 [用户指南](docs/USERS.md)、[设计决定](docs/adr/0012-consumer-profile-dashboard-concurrency.md) 与 [验收报告](docs/acceptance/USER-CENTER-1-2026-11-02.md)。

用户中心第二批新增人工额度调整与结算、兑换码、套餐订阅、邮箱验证与密码找回、GitHub/LinuxDO 绑定登录。当前 schema 10，旧用户默认不启用余额和订阅限制。SMTP/OAuth 需管理员配置后启用；未接在线支付、订单、退款和返利。见 [设计决定](docs/adr/0013-consumer-entitlements-identity.md) 和 [验收报告](docs/acceptance/USER-CENTER-2-2026-11-02.md)。

## 功能特性

| 功能 | 说明 |
| --- | --- |
| 多账号管理 | 浏览器分阶段授权、粘贴回调、预览后确认保存、导入与删除 |
| 账号池调度 | 最少在途、轮询、顺序耗尽，支持粘性会话、手动设置并发、暂停与冷却 |
| 分组路由 | API Key 绑定分组；支持带版本号的统一模型名称，将多个平台的对应型号加入同一账号池调度 |
| 协议兼容 | Chat Completions、Responses、Anthropic Messages，支持流式输出和工具调用 |
| 普通用户 | 管理员创建用户、分配分组与共享预算；统一登录后按角色进入页面、自己的密钥和用量 |
| 管理员登录 | 统一账号登录、后端角色校验、8 小时会话、退出及改密撤销、管理操作审计 |
| API Key 管理 | 创建、停用、删除、按需显示和复制，以及请求数、Token、USD 预算 |
| 用量统计 | 输入、输出、缓存 Token、参考费用、耗时、日期筛选和分页 |
| 模型配置 | 模型目录、别名、USD 价格表、上下文与摘要策略 |
| 摘要恢复 | 完整性检查、有限重试、可选备用模型；Pi 分段恢复由启动器自动安装 |
| 平台独立策略 | custom/passthrough 系统提示词、出站指纹清洗；WorkBuddy 原生搜索，CodeArts/Trae 搜索需配置 SearXNG |
| WorkBuddy 成长任务 | 一键接取、执行、核对进度并领取奖励，支持两个账号并行和失败保护 |
| 用量持久化 | 追加结算账本与原子状态保存，重启恢复已记录用量和预算 |
| 客户端接入 | Pi、Codex、Claude Code 启动器，从网关读取推荐配置 |

上游按账号实际权益与积分计费。USD 价格表用于 API 参考估价，不等同于华为积分账单。Token 和 USD 预算按返回用量结算，单次请求仍可能超额。

## WorkBuddy 成长任务

在「账号池」选择 **WorkBuddy → 成长任务 → 执行成长任务**。已接入的任务自动接取、执行、核对进度并领奖；已有进度按差额补足，已领取任务自动跳过。首次领养需确认协议。同一账号逐项串行，最多两个账号并行；未知结果会停止该账号本轮队列，刷新核对后再操作。

支持领养、活跃、桌面/Web 活动、模板、主题、指定专家、专家团与技能任务。事件任务不调用模型，专家和技能任务使用真实短对话并记录用量，可能消耗积分。主题任务会设置官方账号的和平精英主题。公众号任务需本人关注官方公众号满 24 小时，再刷新领取。

任务清单与奖励由上游按账号下发。2026-10-31 的真实账号验收确认 **19 项中已领取 18 项**，剩余为公众号任务；逐项真实验收与批量页面回归均通过，尚未在全新账号上从零一次跑完整批。详见 [使用与维护说明](docs/WORKBUDDY-GROWTH.md) 和 [真实验收报告](docs/acceptance/GROWTH-REMAINING-2026-10-31.md)。

## 部署

### Windows x64 下载运行

从 [正式发布页](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/latest) 下载 `codearts2api-panel-vX.Y.Z-windows-amd64.zip`，**全部解压后双击 `start.cmd`**，打开 http://127.0.0.1:10087/。无需安装 Go、Node.js、Python 或 Docker。

v0.3.8 起首次运行通过向导设置管理员邮箱和密码。数据保存在 `%LOCALAPPDATA%\CodeArts2API`；升级前按 Ctrl+C 停止并备份整个数据目录，再运行新包的 `start.cmd`。当前为前台运行版，不注册 Windows 系统服务。详细步骤见 [Windows 使用说明](deploy/windows/README.md)。

### 一键安装系统级服务（Linux）

需要 Python 3.10+、systemd 和 sudo。自动下载最新稳定版并校验，创建专用服务用户、注册开机启动：

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash
```

当前 `main` 安装脚本会在可用终端中依次询问语言、监听地址、端口并确认安装；回车使用默认值。通过 `curl | sudo bash` 运行同样可交互。添加 `--non-interactive` 可跳过提问，详细参数见 [部署指南](deploy/README.md#终端交互向导)。

默认安装到 `/opt/codearts2api`，服务名 `codearts2api.service`，监听 `127.0.0.1:10087`。通过 SSH 隧道或 HTTPS 反代访问；需要直接监听所有网卡时，在命令末尾改用 `sudo bash -s -- install --host 0.0.0.0`。服务以不可登录的专用用户运行，程序和升级配置由 root 管理。

```bash
sudo systemctl status codearts2api
# 打开面板，设置管理员邮箱和密码，完成安装向导
# 后续升级（先阅读版本说明）：
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/main/deploy/install.sh | sudo bash -s -- upgrade
```

重复安装会拒绝覆盖。已有用户级或源码实例继续使用原管理方式，需显式迁移时参考 [部署指南](deploy/README.md)。

### 固定版本安装（Linux 服务器）

不需要 Go 或 Node.js，需 Python 3.10+ 和可用的用户级 systemd。从 [正式发布页](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/latest) 下载并查看 `install.sh` 后执行：

```bash
sh install.sh check
sh install.sh install --version v0.3.3 --root "$HOME/codearts2api-managed" --port 10087
# 后续升级：先阅读新版本说明，再执行
sh install.sh upgrade --root "$HOME/codearts2api-managed"
```

安装工具会下载并校验对应架构的程序和运维工具；首次安装通过向导设置 `admin` 密码。默认绑定 loopback，远程访问使用 SSH 隧道或 HTTPS 反代。已有源码实例先按 [迁移说明](deploy/README.md#已有源码服务迁入受管目录) 接入受管目录。

### 方式一：从源码运行

源码构建需要 Git、Go 1.27.3 或更新版本，以及 Node.js 24.11.x 和 npm。构建脚本自动使用固定版本的 pnpm；运行已编译的生产二进制不需要 Node.js。

```bash
git clone --branch main https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
cd codearts2api-panel
./build.sh
./start.sh
```

默认访问地址：[http://127.0.0.1:10087/](http://127.0.0.1:10087/)。

脚本在前台运行，按 `Ctrl+C` 停止；需要开机自启时另行配置 systemd 等服务管理器。

自定义监听地址与端口：

```bash
./start.sh -host 0.0.0.0 -port 10087
```

或使用环境变量：

```bash
CODEARTS_HOST=0.0.0.0 CODEARTS_PORT=10087 ./start.sh
```

修改监听配置需要重启。`0.0.0.0` 表示监听所有接口，浏览器应使用服务器 IP 或域名访问。

### 方式二：Docker Compose

需要 Docker 和 Compose。

```bash
git clone --branch main https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
cd codearts2api-panel
cp .env.example .env
docker compose up -d --build
```

Compose 使用持久化命名卷，默认端口映射为 `127.0.0.1:10087:10087`。如需直接允许远程访问，可将 `docker-compose.yml` 中的映射改为 `10087:10087`，并配置服务器防火墙及安全组。

常用命令：

```bash
docker compose logs -f codearts2api
docker compose restart codearts2api
docker compose down
```

### 域名与公网访问

可使用 `用户 → HTTPS 域名 → Nginx/Caddy → 中转服务` 的部署方式。同机反向代理场景下，网关继续监听 `127.0.0.1:10087` 即可；反代需保留原始 Host、关闭响应缓冲并提供足够的流式读取超时。

服务本身不提供 HTTPS。保持管理员登录、API Key 校验，并关闭匿名调用（本机自动解锁已移除）。反代终止 TLS 时，应由可信反代为管理会话 Cookie 添加 Secure 属性。更多边界见 [安全说明](SECURITY.md) 和 [管理说明](docs/MANAGEMENT.md)。

### 升级与备份

先停止实际写状态的服务，备份完整数据目录（包含用量账本、成长检查点和安装标记），再更新代码并重新构建。不要在运行期间仅复制主 JSON 文件：

```bash
git pull --ff-only
./build.sh
# 重启当前服务；Docker 部署执行：
# docker compose up -d --build
```

备份包含账号凭据，应妥善保存；恢复只写入新目录，不自动覆盖最新凭据或账本。旧版本可能拒绝读取新版状态格式，不能盲目降级。

管理员可在“配置 → 版本与更新”查看运行版本、检查正式更新。`deploy/install.sh` 提供校验后的版本下载/安装入口，已有源码服务可通过迁移工具接入受管升级。

固定版本安装、生产 Docker、HTTPS 模板、备份恢复、升级和健康自检见 [部署与运维指南](deploy/README.md)。工具和模板保留单实例，不增加推理请求路径的处理层。

## 首次使用

1. v0.3.8 起新部署打开面板，根据向导设置管理员邮箱和密码，无需初始化 Token。
2. 完成后打开 `/login`，使用刚设置的管理员邮箱和密码登录。已有部署沿用原密码。
3. 在「账号池」点击「添加账号」，选择平台和「浏览器授权」，点击下一步，再生成链接并复制到另一浏览器授权。
4. 粘贴最终回调地址，提交授权信息，核对账号预览后确认保存。
5. 刷新模型目录，使用实际可用的模型 ID；测活会消耗真实额度。
6. 在「API 密钥」中创建客户端专用密钥，按需设置预算。

### 首次安装与管理员密码

v0.3.8 起，新部署未指定管理密码时，打开面板进入首次安装向导，填写自定义管理员邮箱和至少 12 位密码，不需要初始化 Token 或邮箱验证码。邮箱用作登录名，不会创建真实邮箱，也不代表邮箱归属已验证。

首次安装默认仅监听回环地址。请通过本机或 SSH 隧道完成初始化，再对外开放服务；可访问未初始化实例的人能够设置首次管理员。

安装完成后删除待安装标记，创建 `.installed` 标记；邮箱和密码哈希原子保存。后台任务仅在安装完成后启动，默认调用密钥在登录后的「API 密钥」中查看。已有部署没有配置管理员邮箱时继续使用 `admin` 和原密码。

普通用户由管理员在「用户管理」创建，并绑定分组；普通用户与管理员使用同一登录页，服务端根据身份显示对应界面。

已有部署不会重新进入向导。旧版首次密码记录在 `*.access.json` 的 `admin_password` 字段；该文件不会跟随后续改密更新。旧部署原先没有密码时，恢复文件为 `*.admin-recovery.json`。`state.json` 中的密码哈希不能用于登录。

自动部署可显式设置 `CODEARTS_ADMIN_PASSWORD` 跳过向导，保留原启动行为及私有 `*.access.json` 初始凭据文件；API Key 用于调用模型，与管理员登录密码不同。

忘记密码时，在服务的启动环境中设置新的 `CODEARTS_ADMIN_PASSWORD` 并重启。使用 `start.sh` 时，先停止原服务，再执行：

```bash
CODEARTS_ADMIN_PASSWORD='替换为你的新密码' ./start.sh
```

Docker Compose 部署可在 `.env` 中设置 `CODEARTS_ADMIN_PASSWORD`，然后执行 `docker compose up -d` 让新环境变量生效。不要通过删状态文件来重设密码；安装标记或账本还在时服务会拒绝重新初始化。初始化凭证、恢复文件及初始凭据文件需妥善保管，不要公开上传。

详细登录流程、凭据续期和恢复说明见 [管理文档](docs/MANAGEMENT.md) 与 [面板文档](docs/PANEL.md)。

## API 接口

| 接口 | 用途 |
| --- | --- |
| `POST /v1/chat/completions` | Chat Completions，流式/非流式和工具调用 |
| `POST /v1/responses`，别名 `/responses` | Responses → Chat 转换 |
| `POST /v1/messages` | Anthropic Messages → Chat 转换 |
| `POST /v1/search/messages` | Messages 形态的联网搜索；按平台配置搜索后端 |
| `GET /v1/models` | 模型目录与可用的客户端推荐配置 |
| `GET /v1/client-profile` | 指定模型的客户端配置建议 |
| `GET /v1/capabilities` | 网关能力及上游协议说明 |
| `/admin/api/*`、`GET /metrics` | 需管理员鉴权的管理接口与指标 |
| `GET /healthz` | 无需鉴权的服务健康检查 |

`/v1/*` 使用调用密钥。示例模型需替换为账号实际可用的模型：

```bash
export CODEARTS_API_KEY='你的调用密钥'
curl http://127.0.0.1:10087/v1/chat/completions \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

Responses 为协议转换，当前不支持原生 compact、WebSocket、`store=true` 和 `previous_response_id`。模型别名不代表对应厂商的真实模型。兼容范围和工具调用限制见 [Responses 文档](docs/RESPONSES.md)。

Trae 在账号池点击「添加账号」并选择「Trae CN / SOLO」，按提示填写设备信息和授权回调，或选择导入完整凭据，模型名使用 `trae/cn/<config_name>`。接入和验收限制见 [Trae 说明](docs/TRAE.md)。

## 客户端接入

```bash
python3 scripts/connect-client.py pi --url http://127.0.0.1:10087
python3 scripts/connect-client.py codex --url http://127.0.0.1:10087
python3 scripts/connect-client.py claude --url http://127.0.0.1:10087
```

密钥通过隐藏输入或 `CODEARTS_API_KEY` 环境变量提供。启动器读取模型推荐参数，为 Pi 自动安装受控摘要恢复适配，为 Claude Code 自动省略尚不支持的思考控制参数；Codex 在 Linux 上先检查本机沙箱。只验证配置可加 `--check`。

详见 [客户端接入说明](docs/CLIENTS.md)。客户端版本及本机权限会影响实际兼容性，完整结果见 [验收快照](docs/acceptance/QUALITY-SNAPSHOT.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [面板与账号池](docs/PANEL.md) | 登录流程、账号调度、冷却和故障处理 |
| [WorkBuddy 专用功能](docs/WORKBUDDY-FEATURES.md) | 联网搜索、系统提示词、出站指纹清洗与配置 |
| [WorkBuddy 成长任务](docs/WORKBUDDY-GROWTH.md) | 集中入口、领养前置与依赖处理、批量接取/领奖、桌面/Web 活动、专家/技能执行与奖励核验 |
| [分组统一模型](docs/GROUP-MODELS.md) | 版本化公开名称、多个平台来源、隐藏原型直调及调度边界 |
| [各平台请求策略](docs/PROVIDER-FEATURES.md) | 独立提示词、清洗、搜索后端配置与维护边界 |
| [管理与预算](docs/MANAGEMENT.md) | 管理会话、审计、凭据续期、密钥预算与恢复 |
| [用量与价格](docs/USAGE.md) | Token 构成、USD 计价、日期查询和分页 |
| [客户端接入](docs/CLIENTS.md) | Pi、Codex、Claude Code 配置与限制 |
| [协议兼容](docs/RESPONSES.md) | Responses 转换、工具和推理历史 |
| [Sub2API 设计参考](docs/SUB2API-ALIGNMENT.md) | 已实现能力及未覆盖部分 |
| [项目结构设计 v1](docs/architecture/README.md) | 当前迁移进度、目标目录、模块边界与验收 |
| [质量验收](docs/acceptance/QUALITY-PLAN.md) | 验收方法、门槛及复跑步骤 |
| [安全策略](SECURITY.md) | 漏洞反馈、部署边界与数据保护 |

## 开发与贡献

Go 工程位于 `backend/`，服务入口为 `backend/cmd/panel`，诊断入口为 `backend/cmd/probe`。当前页面继续内嵌于后端；独立前端工程将在后续阶段迁移。根目录保留构建、启动、Docker 与部署入口，详见 [开发约定](AGENTS.md) 和 [部署示例](deploy/README.md)。

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...
cd ..
python3 -m unittest discover -s scripts -p test_connect_client.py
node scripts/pi/codearts-compaction.test.mjs
```

Go 服务仅依赖标准库。Pi 适配测试使用 Node.js 24；浏览器检查另外需要 Playwright 和浏览器。真实上游验收会消耗账号额度，执行方法见验收文档。

欢迎提交 Issue 和 Pull Request。每次修改的原因、行为变化、测试结果和已知限制写入提交说明；README 维护当前使用方法。规范见 [CONTRIBUTING.md](CONTRIBUTING.md)，历史见 [Git 提交记录](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/commits/main/)。

## 贡献者与致谢

<table>
  <tr>
    <td align="center">
      <a href="https://github.com/aiprofesserzhaofeng8-jpg"><img src="https://github.com/aiprofesserzhaofeng8-jpg.png?size=80" width="64" height="64" alt="项目维护者头像"></a><br>
      <a href="https://github.com/aiprofesserzhaofeng8-jpg">aiprofesserzhaofeng8-jpg</a><br>
      项目维护
    </td>
    <td align="center">
      <a href="CONTRIBUTORS.md#ai-辅助开发贡献"><img src="https://github.com/openai.png?size=80" width="64" height="64" alt="OpenAI 工具标识"></a><br>
      OpenAI ChatGPT / Codex<br>
      AI 辅助开发工具署名
    </td>
  </tr>
</table>

| 贡献者 / 项目 | 贡献方式 |
| --- | --- |
| [aiprofesserzhaofeng8-jpg](https://github.com/aiprofesserzhaofeng8-jpg) | 项目需求、使用反馈、仓库维护与发布 |
| **OpenAI ChatGPT / Codex** | AI 辅助开发贡献：方案讨论、代码实现、问题排查、测试与文档编写 |
| [CodeArts2API](https://github.com/2951461586/CodeArts2API) 及其贡献者 | 基础码道协议、签名、登录与服务实现 |
| [WorkBuddy2API Panel](https://github.com/linguo2625469/workbuddy2api-panel) 及原作者 | 账号管理产品思路和面板视觉参考 |
| [Sub2API](https://github.com/Wei-Shaw/sub2api) 及其贡献者 | Responses 转换库、管理设计与 README 组织方式参考 |

AI 署名记录使用 OpenAI 工具进行辅助开发的事实，不表示 OpenAI 公司参与维护、官方合作或背书。更多贡献说明见 [CONTRIBUTORS.md](CONTRIBUTORS.md)。

## 许可证

基础项目采用 [Apache-2.0](LICENSE)。WorkBuddy 相关设计来源保留 [MIT 声明](LICENSE.workbuddy)，[Qoder 接入](docs/QODER.md)的协议参考来源保留 [MIT 声明](LICENSE.qoder)，[Kiro 接入](docs/KIRO.md)的协议参考来源保留 [MIT 声明](LICENSE.kiro)，`backend/internal/responsescompat` 中的复用代码采用 [LGPL-3.0](backend/internal/responsescompat/LICENSE)，附带的 Ubuntu AppArmor profile 保留其 GPL-2.0-or-later 声明。

各组件的来源、修改声明和许可证边界见 [NOTICE](NOTICE)。本项目为独立社区项目。
