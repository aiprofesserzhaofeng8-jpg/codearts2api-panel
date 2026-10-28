<div align="center">

# CodeArts2API Panel

**码道 / WorkBuddy 多账号 API 网关与管理面板**

将码道和 WorkBuddy 账号接入 Chat Completions、Responses 和 Anthropic Messages 兼容接口。

**中文** · [English](README_EN.md)

[![CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?logo=go)](go.mod)

[快速部署](#部署) · [客户端接入](#客户端接入) · [使用文档](#文档) · [提交记录](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/commits/develop/) · [贡献者](#贡献者与致谢)

</div>

> 当前为 `develop` 开发版：已加入 WorkBuddy 原生适配，Trae 接口预留但尚未实现。原码道基线固定在 [v0.1.0](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/tag/v0.1.0)。接入方法和验收边界见 [多上游说明](docs/MULTI-PROVIDER.md)。

## 项目简介

CodeArts2API Panel 为个人自用提供码道账号管理、模型请求转发、协议转换与用量统计。服务使用 Go 标准库开发，管理页面嵌入二进制，状态保存在本地 JSON 文件中。

支持在本机、服务器或 Docker 中运行，可设置监听地址与端口，也可通过 HTTPS 反向代理提供公网访问。当前采用单进程、单管理员架构，不支持多个实例共享同一状态文件。

## 功能特性

| 功能 | 说明 |
| --- | --- |
| 多账号管理 | 浏览器分阶段授权、粘贴回调、预览后确认保存、导入与删除 |
| 账号池调度 | 最少在途、轮询、顺序耗尽，支持粘性会话、并发限制、暂停与冷却 |
| 协议兼容 | Chat Completions、Responses、Anthropic Messages，支持流式输出和工具调用 |
| 管理员登录 | 密码哈希、8 小时会话、退出及改密撤销、管理操作审计 |
| API Key 管理 | 创建、停用、删除、按需显示和复制，以及请求数、Token、USD 预算 |
| 用量统计 | 输入、输出、缓存 Token、参考费用、耗时、日期筛选和分页 |
| 模型配置 | 模型目录、别名、USD 价格表、上下文与摘要策略 |
| 摘要恢复 | 完整性检查、有限重试、可选备用模型；Pi 分段恢复由启动器自动安装 |
| 客户端接入 | Pi、Codex、Claude Code 启动器，从网关读取推荐配置 |

上游按账号实际权益与积分计费。USD 价格表用于 API 参考估价，不等同于华为积分账单。Token 和 USD 预算按返回用量结算，单次请求仍可能超额。

## 部署

### 方式一：从源码运行

需要 Git 和 Go 1.27.3 或更新版本。

```bash
git clone --branch develop https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
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
git clone --branch develop https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
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

服务本身不提供 HTTPS。保持管理员登录、API Key 校验，并关闭本机自动解锁及匿名调用。反代终止 TLS 时，应由可信反代为管理会话 Cookie 添加 Secure 属性。更多边界见 [安全说明](SECURITY.md) 和 [管理说明](docs/MANAGEMENT.md)。

### 升级与备份

先备份状态文件或 Docker 数据卷，再更新代码并重新构建：

```bash
git pull --ff-only
./build.sh
# 重启当前服务；Docker 部署执行：
# docker compose up -d --build
```

备份包含账号凭据，应妥善保存。升级及回退要求见对应提交说明；旧版本可能拒绝读取新版状态格式。

## 首次使用

1. 查看首次生成的管理员密码和 API Key。
2. 打开面板，使用 `admin_password` 登录。
3. 在「账号池」中打开浏览器登录窗口，生成链接并复制到另一浏览器授权。
4. 粘贴最终回调地址，提交授权信息，核对账号预览后确认保存。
5. 刷新模型目录，使用实际可用的模型 ID；测活会消耗真实额度。
6. 在「API 密钥」中创建客户端专用密钥，按需设置预算。

### 管理员登录密码在哪里？

首次启动会生成管理员密码。打开下面的初始凭据文件，使用 **`admin_password` 字段的值**登录管理面板；`api_key` 是客户端调用接口的密钥，不用于管理员登录。

**源码 / start.sh 部署**：在项目目录查看 `data/state.json.access.json`：

```bash
cat data/state.json.access.json
```

**Docker Compose 部署**：查看容器内 `/data/codearts2api-data.json.access.json`：

```bash
docker compose exec codearts2api cat /data/codearts2api-data.json.access.json
```

如果启动时已指定 `CODEARTS_ADMIN_PASSWORD`，请使用自己设置的密码。初始凭据文件不会随之后的改密同步更新；`state.json` 中保存的是密码哈希，不能用它作为登录密码。

忘记密码时，在服务的启动环境中设置新的 `CODEARTS_ADMIN_PASSWORD` 并重启。使用 `start.sh` 时，先停止原服务，再执行：

```bash
CODEARTS_ADMIN_PASSWORD='替换为你的新密码' ./start.sh
```

Docker Compose 部署可在 `.env` 中设置 `CODEARTS_ADMIN_PASSWORD`，然后执行 `docker compose up -d` 让新环境变量生效。初始凭据文件包含真实密码和调用密钥，请妥善保管，不要公开上传。

详细登录流程、凭据续期和恢复说明见 [管理文档](docs/MANAGEMENT.md) 与 [面板文档](docs/PANEL.md)。

## API 接口

| 接口 | 用途 |
| --- | --- |
| `POST /v1/chat/completions` | Chat Completions，流式/非流式和工具调用 |
| `POST /v1/responses`，别名 `/responses` | Responses → Chat 转换 |
| `POST /v1/messages` | Anthropic Messages → Chat 转换 |
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

## 客户端接入

```bash
python3 scripts/connect-client.py pi --url http://127.0.0.1:10087
python3 scripts/connect-client.py codex --url http://127.0.0.1:10087
python3 scripts/connect-client.py claude --url http://127.0.0.1:10087
```

密钥通过隐藏输入或 `CODEARTS_API_KEY` 环境变量提供。启动器读取模型推荐参数，为 Pi 自动安装受控摘要恢复适配；Codex 在 Linux 上先检查本机沙箱。只验证配置可加 `--check`。

详见 [客户端接入说明](docs/CLIENTS.md)。客户端版本及本机权限会影响实际兼容性，完整结果见 [验收快照](docs/acceptance/QUALITY-SNAPSHOT.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [面板与账号池](docs/PANEL.md) | 登录流程、账号调度、冷却和故障处理 |
| [管理与预算](docs/MANAGEMENT.md) | 管理会话、审计、凭据续期、密钥预算与恢复 |
| [用量与价格](docs/USAGE.md) | Token 构成、USD 计价、日期查询和分页 |
| [客户端接入](docs/CLIENTS.md) | Pi、Codex、Claude Code 配置与限制 |
| [协议兼容](docs/RESPONSES.md) | Responses 转换、工具和推理历史 |
| [Sub2API 设计参考](docs/SUB2API-ALIGNMENT.md) | 已实现能力及未覆盖部分 |
| [质量验收](docs/acceptance/QUALITY-PLAN.md) | 验收方法、门槛及复跑步骤 |
| [安全策略](SECURITY.md) | 漏洞反馈、部署边界与数据保护 |

## 开发与贡献

```bash
go test ./...
go test -race ./...
go vet ./...
python3 -m unittest discover -s scripts -p test_connect_client.py
node scripts/pi/codearts-compaction.test.mjs
```

Go 服务仅依赖标准库。Pi 适配测试使用 Node.js 24；浏览器检查另外需要 Playwright 和浏览器。真实上游验收会消耗账号额度，执行方法见验收文档。

欢迎提交 Issue 和 Pull Request。每次修改的原因、行为变化、测试结果和已知限制写入提交说明；README 维护当前使用方法。规范见 [CONTRIBUTING.md](CONTRIBUTING.md)，历史见 [Git 提交记录](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/commits/develop/)。

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

基础项目采用 [Apache-2.0](LICENSE)。WorkBuddy 相关设计来源保留 [MIT 声明](LICENSE.workbuddy)，`internal/responsescompat` 中的复用代码采用 [LGPL-3.0](internal/responsescompat/LICENSE)，附带的 Ubuntu AppArmor profile 保留其 GPL-2.0-or-later 声明。

各组件的来源、修改声明和许可证边界见 [NOTICE](NOTICE)。本项目为独立社区项目。
