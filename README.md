# CodeArts2API Panel

华为云码道多账号网关：浏览器登录、多账号池、OpenAI / Anthropic 兼容接口、Web 管理面板。

本项目采用 [WorkBuddy2API Panel](https://github.com/linguo2625469/workbuddy2api-panel) 的产品思路，基于 [2951461586/CodeArts2API](https://github.com/2951461586/CodeArts2API) 的码道签名、登录和协议实现开发；基础版本固定为 `c11aa7b`，基础代码沿用 Apache-2.0 许可证。Responses 转换层复用 Sub2API 的 LGPL-3.0 代码，来源与许可证见 [NOTICE](NOTICE) 和 [转换层来源](internal/responsescompat/UPSTREAM.md)。

## 当前状态

已实现可运行的网关、面板和自动化测试。已使用真实账号验证 Chat Completions、Responses 流式/非流式生成，以及 Pi 的 Responses 工具调用闭环；登录、续期的可用性仍取决于账号凭据类型及华为上游。

参考协议记录显示，旧 benefit 免费 Token 通道已于 2026-10 下线。本项目按当前积分通道工作，不承诺旧教程中的免费额度；实际可用模型及费用以你的账号和上游返回为准。

## 快速启动

从 GitHub 获取源码：

```bash
git clone https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
cd codearts2api-panel
./build.sh
./start.sh
```

需要 Go 1.27.3 或更新版本。管理员密码和 API Key 在本机首次启动时生成，不包含在源码仓库中。

已编译时：

```bash
./start.sh
```

`start.sh` 在前台运行，按 Ctrl+C 停止。需要开机自启时，可将它配置为本机的 systemd 服务；脚本本身不创建系统服务。

从源码构建需要 Go 1.27.3 或更新版本：

```bash
./build.sh
./start.sh
```

默认只监听 `127.0.0.1:10087`。打开 **http://127.0.0.1:10087/**。

1. 打开面板后输入管理员密码登录；默认不再自动信任本机访问。登录会话有效期 8 小时，退出、服务重启或修改密码后失效。
2. 首次启动生成的 `data/state.json.access.json` 包含模型调用的 `api_key` 和管理面板使用的 `admin_password`。旧部署未设置密码时，会自动生成 `data/state.json.admin-recovery.json`。
3. 进入「账号」→「浏览器登录」，先弹出添加账号窗口，点击「生成登录链接」，复制到另一个浏览器或无痕窗口授权。
4. 回到窗口粘贴最后的回调 URL、OAuth 授权码或完整 session JSON，点击「提交授权信息」。如果本机已经自动收到回调，可直接点「检查授权信息」。核对账号预览后点击「确认完成登录」才会保存；已有账号须勾选确认更新。
5. 回到概览刷新模型清单，选择实际返回的模型 ID。模型测活会发起真实请求并消耗账号额度。
6. 客户端填写 Base URL `http://127.0.0.1:10087/v1` 和调用密钥。

启动脚本不自动导入桌面登录态。Windows 可在面板显式导入；Linux/macOS 使用浏览器登录或 session JSON。首次初始化默认关闭自动领取活动福利、向 IDE 回写凭据，均可在设置中启用。

同机的隐私浏览器也可能自动送达回调：同一回调随后再次粘贴时会复用已有授权预览，不重复换证。重新打开尚未结束的登录窗口会检查当前状态，账号仍须明确确认后才保存。失败、过期或取消时会保留具体原因。

浏览器登录支持 OAuth 授权码和旧版登录票据两种回调；旧版票据只返回临时 STS 凭据，没有 refresh token，到期需重新登录。面板刷新后会恢复当前标签页尚未过期的登录流程。登录状态显示当前步骤和错误；服务日志只记录步骤，不记录授权码、票据或密钥。

查看初始访问凭据：

```bash
cat data/state.json.access.json
```

这个文件记录的是首次生成值，之后修改密码或密钥不会同步更新它。忘记管理员密码时，可用 `CODEARTS_ADMIN_PASSWORD='新密码' ./start.sh` 重设，然后在面板新建调用密钥。

## 本次新增的管理能力

管理员登录与密码哈希、凭据续期冲突保护、账号模型独立健康状态、管理审计、摘要备用模型和密钥使用预算已加入。入口、预算边界与恢复方法见 [管理增强说明](docs/MANAGEMENT.md)。

## 接口

| 路径 | 用途 | 鉴权 |
| --- | --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions，流式/非流式、工具调用、推理内容 | 调用密钥 |
| `POST /v1/responses`（别名 `/responses`） | Responses → Chat 转换，流式/非流式、工具、推理历史 | 调用密钥 |
| `POST /v1/messages` | Anthropic Messages，文本/工具/思考内容转换 | 调用密钥 |
| `GET /v1/models` | 模型列表，可能含上游不可达时的兜底目录 | 调用密钥 |
| `/admin/api/*` | 账号、密钥、配置、用量和日志 | 管理员会话；兼容密码请求头 |
| `GET /metrics` | Prometheus 指标 | 管理员密码 |
| `GET /healthz` | 进程状态和当前可调度账号数 | 无 |

Responses 能力、限制与示例见 [docs/RESPONSES.md](docs/RESPONSES.md)。模型以 `/v1/models` 为准；继承的通用别名会映射到码道模型，例如 `gpt-4o` 并不代表上游提供 OpenAI GPT-4o，建议直接使用码道的模型 ID。

```bash
export CODEARTS_API_KEY='从访问凭据文件或面板取得的调用密钥'
curl http://127.0.0.1:10087/v1/models \
  -H "Authorization: Bearer $CODEARTS_API_KEY"

curl http://127.0.0.1:10087/v1/chat/completions \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  -H 'X-Session-Id: my-conversation-1' \
  -d '{"model":"glm-5.3","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

示例模型需替换为账号实际可用的模型。上游对部分模型只接受流式请求，网关会在需要时聚合为非流式结果。

「密钥」页支持复制所有已保存的调用密钥，包括之前创建的密钥。默认以密码输入框隐藏，点击「显示」可查看和手动选中，点击「复制」复制完整值；刷新页面后仍可复制。完整密钥仅在点击「复制 / 显示」时通过管理鉴权后的专用接口获取；概览和列表只返回脱敏值，隐藏或离开密钥页会清除输入框中的完整值。普通调用密钥不能读取管理接口。

## 用量与模型页面

侧栏「用量」和「模型」分别展示统计与模型目录。用量页参考 Sub2API 的四卡片布局，显示总请求、总 Token、总消费和平均耗时，并提供 Token 构成、请求成功率与分页用量记录；活动福利移至账号页。

Chat Completions、Responses、Messages 的流式/非流式调用均记录上游返回的 usage。输入 Token 包含缓存、输出 Token 包含推理；卡片的输入明细扣除缓存，`总 Token = 输入（未缓存）+ 缓存 + 输出`，避免重复计数。流式累计 usage 按最终快照记录，不逐帧相加。

旧记录没有 Token 或耗时汇总时不估算补齐，页面显示统计覆盖数；新统计从升级后的请求开始。平均耗时基于已采集的请求全程耗时（包含失败请求）。累计值随状态文件保存。用量页汇总与明细统一查询独立用量历史，翻页不改变汇总；清空运行日志、重置累计计数不会删除用量历史。

「模型 → Token 价格表」默认只读，点击「编辑」弹出窗口，填写后保存，取消不修改数据。统一使用 USD，可为实际模型设置未缓存输入、输出和缓存命中的每百万 Token 单价。「用量」按请求返回的真实 usage（包括失败和重试的已报告用量）计算 **API 估算费用**，不是华为积分账单。美元统一累计；未定价或缺少 usage 的请求不计价，页面显示覆盖数量。修改价格只影响后续完成的请求，日志保留当时单价快照；旧请求不自动追溯估价。

默认仅预填已核实的 DeepSeek V4.1 Flash 官方高峰参考价（2026-10-26 核对）：输入 $0.30、缓存 $0.006、输出 $1.20 / 百万 Token。固定采用高峰参考价，不自动切换官方峰谷时段；官方空闲价为其一半。来源：[DeepSeek 模型与价格](https://api-docs.deepseek.com/quick_start/pricing/)。其他模型可手动配置，空白表示未知，0 表示明确免费。按实际解析后的上游模型计价，模型别名不单独定价。

公式：`估算费用 = ((输入 - 缓存) × 输入单价 + 缓存 × 缓存单价 + 输出 × 输出单价) / 1,000,000`。推理 Token 已包含在输出中，不重复计费。此价格表参照 Sub2API 的本地定价方式，当前不自动同步 LiteLLM、不处理长上下文阶梯或峰谷日历。

界面验证：`PLAYWRIGHT_CHANNEL=chrome node scripts/usage-smoke.cjs`，使用模拟数据验证卡片数值、页面分离、历史缺失值和手机布局，不调用真实上游。

## 账号池

面板参考 WorkBuddy2API Panel 的紧凑布局和配色，默认进入账号池。顶部统计账号可用、冷却、暂停和并发状态；支持搜索和状态筛选、复制 API 地址。右上角切换明暗主题并自动记住选择，手机端以账号卡片展示操作。

面板「设置 → 账号池调度」可即时修改：

| 参数 | 默认值 | 含义 |
| --- | --- | --- |
| strategy | least_inflight | 最少在途、轮询、顺序耗尽三选一 |
| max_in_flight | 2 | 每个账号同时处理的生成请求数 |
| sticky_ttl_sec | 1800 | 会话绑定时长，0 关闭 |
| rate_cooldown_sec | 60 | 限流后的冷却，0 关闭 |
| breaker_threshold | 3 | 连续失败熔断阈值 |
| breaker_cooldown_sec | 300 | 熔断恢复等待 |

账号页显示在途请求、冷却剩余时间，支持暂停选号、恢复选号、解除冷却与禁用。暂停只影响模型请求，保留账号维护功能。冷却或并发满时换号；全池不可用返回 503。流式已经输出内容后不会重新生成，避免拼接两次回答。请求超时包含全部重试。

会话通过 `X-Session-Id`、`conversation_id` 或首条消息哈希绑定，按调用密钥隔离。Chat 透传不替换客户端的 system/developer 提示词；Responses 路径沿用 Sub2API 的角色归一化规则，合并开头的指令，兼容只接受开头 system 消息的模型。详细规则见 [docs/PANEL.md](docs/PANEL.md)。

## Docker

```bash
docker compose up -d --build
```

Compose 使用持久化命名卷，并只映射到本机 `10087` 端口。首次生成的管理密码和调用密钥位于卷内：

```bash
docker compose exec codearts2api cat /data/codearts2api-data.json.access.json
```

浏览器登录的回调服务在容器内。面板支持粘贴浏览器最终回调 URL；也可固定回调端口并配置端口映射/SSH 转发。不能把容器内的 `127.0.0.1` 当成宿主机回调监听器。

## 开发与验证

```bash
go test ./...
go test -race ./...
go vet ./...
# 可选：下载固定版本 Sub2API 的回归用例，在隔离临时模块中验证转换层
python3 scripts/check-sub2api-compat.py
node --check internal/server/web/console.js
```

可选浏览器检查（需要本机安装 Playwright 和对应浏览器）：`node scripts/ui-smoke.cjs`。使用已安装的 Chrome 时设置 `PLAYWRIGHT_CHANNEL=chrome`。检查会保存并恢复调度策略，账号操作使用浏览器模拟数据，不导入账号或调用模型。

登录入口检查：`PLAYWRIGHT_CHANNEL=chrome node scripts/login-smoke.cjs`，验证先显示模态窗口，再生成链接，只有主动点击链接才打开官方登录页，不输入华为凭据。`scripts/account-dialog-smoke.cjs` 使用模拟账号验证授权预览、明确保存和删除弹窗（含失败重试）。

项目只依赖 Go 标准库，面板资源嵌入二进制。`data/`、`bin/`、`.env` 和访问凭据文件均被 Git 忽略。账号凭据以受文件权限保护的 JSON 保存，未做磁盘加密；备份目录也包含登录材料。

单实例运行，不要让两个进程同时读写同一状态文件。真实上游协议会变化，协议参考见 [docs/UPSTREAM.md](docs/UPSTREAM.md)。原项目完整说明保留在 [docs/UPSTREAM-README.md](docs/UPSTREAM-README.md)。

2026-10-26：按用户要求统一 USD。此次将已有人民币估算记录备份后按同一 Token 用量及官方美元参考价重算；日后修改单价仍不改变历史。

### 时间查询和分页

用量页支持全部记录、今天、昨天、近 7 天、近 30 天、本月、上月及自定义日期。日期按浏览器本地时区转换为 Unix 秒，查询区间为「开始日零点（含）到结束日次日零点（不含）」。卡片、Token 构成、成功率和明细使用同一筛选条件，汇总覆盖全部匹配记录而非仅当前页。

分页支持每页 20/50/100 条、上一页/下一页、页码及跳转；改变日期或每页条数回到第 1 页。每页条数保存在本机浏览器。`GET /admin/api/usage?start=...&end=...&page=1&page_size=20` 需要管理鉴权。

独立用量历史最多保存最近 100,000 条元信息，包含当时的价格快照。升级时导入仍保留的运行日志；升级前已经被清理的明细无法恢复，因此「全部记录」表示全部已保存的用量记录，可能少于旧累计请求总数。它随状态文件持久化，不包含对话正文。

分页和日期交互参考 Sub2API 的 `Pagination.vue`、`DateRangePicker.vue` 和 `UsageView.vue`，按本项目纯 HTML/JS 技术栈实现。浏览器回归：`PLAYWRIGHT_CHANNEL=chrome node scripts/usage-smoke.cjs`。

## 主流客户端一键接入

跨机器接入 Pi、Codex、Claude Code 可使用 `python3 scripts/connect-client.py <pi|codex|claude> --url <网关地址>`，启动器自动获取推荐的模型和压缩参数，密钥通过隐藏输入或环境变量提供。Pi 摘要适配已迁入服务端。详见 [docs/CLIENTS.md](docs/CLIENTS.md)。

## 兼容策略与摘要完整性

已增加每模型客户端策略、统一能力声明、摘要完整性保护、同账号有限截断重试和请求级诊断。模型页可编辑上下文/输出/压缩参数；失败摘要不会伪装为完整结果，已消耗的可测量 Token 会累计。完整对照、验证范围和未覆盖部分见 [docs/SUB2API-ALIGNMENT.md](docs/SUB2API-ALIGNMENT.md)。

## 质量验收

评估维度、门槛、复跑命令与真实调用预算见 [验收方案](docs/acceptance/QUALITY-PLAN.md)，逐项结果见 [验收结果快照](docs/acceptance/QUALITY-SNAPSHOT.md)。报告明确区分通过、失败、环境阻塞和等待长时观察，不能将自动化通过等同于完整验收通过。
