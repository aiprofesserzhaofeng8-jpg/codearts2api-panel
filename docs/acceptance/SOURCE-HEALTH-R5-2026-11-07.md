# R5：来源状态与故障恢复验收

日期：2026-11-07。源码位置：`/home/white/codearts2api-panel`。

结论：R5 代码、隔离故障注入与浏览器验收完成；真实平台验收受阻，因此整个 R5 标为部分完成。没有部署、推送或发布新版本，不能据此宣称线上故障已经恢复。

## 实现范围

统一模型的管理员编辑页面分别展示以下信息，禁止相互推断：

| 信息 | 数据来源与范围 | 缺失时的行为 |
| --- | --- | --- |
| 目录存在、来源启用 | 既有 Registry 与统一模型配置 | 缺失目录保留配置，不伪造可用来源 |
| 图片、工具能力 | R2 同一精确来源能力计算 | 未知，不根据健康或一次文本成功补为支持 |
| 账号调度快照 | Store 原锁内读取账号启停、暂停、账号/模型冷却和并发占位 | 零账号显示零；不是当前分组的授权或准入保证 |
| 最近来源请求 | 既有有界日志中的精确来源、HTTP 状态、错误类别与时间 | 尚无记录；不输出请求内容、密钥或原始错误正文 |
| 文本测活 | 当前页面显式执行的测活结果及时间 | 无记录时 last_probe 保持 null；普通请求不冒充测活 |
| 额度与订阅窗口 | 既有账号额度查询的 60 秒缓存快照 | 无记录或查询失败显示未知，不把未知显示为零 |

额度可以显示旧观测，但必须标记已过期及查询时间。订阅窗口只使用上游已提供的剩余、利用率和重置时间，未知重置时间不补造。最近请求只代表日志保留范围内的一次请求；本页测活结果关闭/重载后不持久化，不能当作图片或工具验收。所有状态仅管理员可读；推理仍走 Store 原子权限与准入入口。

该页面通过既有详情 GET 返回状态，不触发额外额度查询、凭据刷新或模型生成。没有新增状态 schema、持久化文件、模型注册中心、健康缓存、锁域、轮询或后台任务。额外工作仅发生在管理员读取详情时：遍历该模型来源与账号，反向扫描现有有界日志；普通推理不执行这段投影。

## 有限恢复与错误分类

- ChatGPT、Claude、Kiro、Qoder 的 HTTP 失败保留合法 Retry-After：429/503 的正秒数或 HTTP-date；过去、格式错误及超过 7 天的值忽略，沿用原有冷却策略。它表示允许重试的时间，不冒充配额重置时间。
- 401 继续使用账号鉴权故障处理；429、404、上游 5xx 继续使用模型级健康。请求 400 与权限 403 不因此冷却账号或模型。模型 404 保留原错误状态，不改为鉴权错误。
- 无明确重试时间的 WorkBuddy 短暂 5xx 仍使用原短恢复窗口；有明确时间时优先尊重上游。没有增加尝试次数、总超时、换端点重试或强制清错机制。
- 冷却到期由原准入入口自动重新接受候选，不需要重启服务、清除错误或新计时任务。手动暂停、停用、分组权限和预算仍在原入口复核。
- 已发送给客户端的分片不重放；取消、预算失败与错误释放原租约，计量继续沿用原请求/尝试账本。没有新增账本或再次结算路径。

Kiro 不再因空文本事件先提交角色分片/HTTP 200；错误类型或 null 的推理内容明确失败，空正文、CRC 损坏、半帧、未完成工具参数继续失败。隔离测试覆盖“空事件后传输截断，下一请求恢复”，不手工清除健康状态。完整 AWS 帧边界上的正常 HTTP EOF 仍按该协议结束处理；上游若在完整帧后错误地正常关闭连接，现有协议无法辨别，不能假造额外终止事件来宣称解决。

## 隔离验证

- providerhttp：Retry-After 秒数/日期、非法/过去/超界值与错误类别。
- storage：状态互相独立、读取无副作用、日志脱敏；假时钟推进冷却后重新实际准入，暂停/停用仍阻止准入，租约幂等释放。
- accounts：旧额度只作为过期快照，缺失/失败不变成零余额。
- server：管理员详情展示能力、健康、额度且不调用上游；ChatGPT 429 的 Retry-After 从 HTTP 经过适配器写入既有模型健康，冷却期间不再请求上游；Kiro 流式/非流式断流后下次请求无需重启即可成功。
- 既有恢复/取消/预算/权限/用量回归：总尝试限制、输出后不重放、取消释放和预算复核保持通过。
- 前端：58 项测试通过，包含来源信号分离、额度未知与过期标记；API 生成类型同步。
- 浏览器：真实应用管理 API、合成上游；覆盖状态面板、不自动测活、显式文本测活、桌面/320px/390px、主题、来源禁用、权限失败保留草稿及并发修改冲突。未接真实上游。
- 完整 make check、5 个包的专项 race、5 份文档的本地链接与 git diff --check 全部通过；不以真实平台失败替代隔离验证。

复用命令（先构建前端，Go 并发受限）：

```bash
GOMAXPROCS=2 GOFLAGS=-p=2 make check \
  GO=/home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go

GOMAXPROCS=2 GOFLAGS=-p=2 \
  /home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go \
  -C backend test -race ./internal/accounts ./internal/storage \
  ./internal/providerhttp ./internal/kiro ./internal/server \
  -run 'Test(Source|QuotaSnapshot|RetryAfter|ChatGPT429|KiroFailedStream|FailureResponse|EmptyFrames|WorkBuddyRecoveryCancellationAndBudget)' -count=1

CODEARTS_U5_BROWSER=1 \
CODEARTS_U5_ARTIFACTS=artifacts/r5-health-2026-11-07/browser \
GOMAXPROCS=2 GOFLAGS=-p=2 \
  /home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go \
  -C backend test ./internal/app -run '^TestUnifiedModelsBrowser$' -v -count=1
```

## 真实平台：4 项受阻

本次调用实际运行的旧版服务 `http://127.0.0.1:10087`，每个名称一次最小生成、有限超时和输出。只读取已有 Pi 密钥，不改模型、分组、客户端配置或部署。正常请求仍会产生用量、日志与既有健康记录。请求使用合成测试常量，未上传用户文件。

| 公开名称 | 观测 | 结论 |
| --- | --- | --- |
| kiro-auto | HTTP 502，无效或不完整的 Kiro 响应 | BLOCKED，未取得原生事件证据，不能归因已定或声称补丁已修复线上 |
| claude-haiku-4.5 | HTTP 502，同样来自 Kiro 入口 | BLOCKED，不代表原生 Claude 接口或模型不支持 |
| gpt-5.6-sol | HTTP 429；网关响应未提供 Retry-After | BLOCKED，不能推断额度、重置时间或恢复时刻 |
| trae-custom_model_deepseek_chat | 上游 HTTP 200、业务码 3003；网关 HTTP 502 | BLOCKED，不把上游 HTTP 200 算作推理成功 |

当前服务的数据目录需特权读取，本轮没有可用的非交互特权，未取得其 Kiro 原生事件或上游头。因此以上观测只用于登记真实阻塞，不能验证尚未部署的新解析器及 Retry-After 透传。后续需在备份后部署候选版本，再用相同名称复核；若仍失败，应检查脱敏的上游错误/事件证据，不能靠增加重试或伪造成功解决。

本机私有产物：`artifacts/r5-health-2026-11-07/`，包括四项结果与浏览器截图；真实测试产物不提交 Git。日志：`/tmp/r5-check-final.log`、`/tmp/r5-race-final.log`、`/tmp/r5-browser-final.log`。

## 部署与回滚边界

修改前源代码备份：`/home/white/backups/codearts2api/before-r5-20261107-192949/source-changes.tar.gz`。备份保留此前未提交的 R1–R4 修改，本次没有清理或覆盖它们。

系统服务保持 active，PID 2102403，启动时间为 2026-11-06 22:29:46 CST；仍运行 `/opt/codearts2api/current/codearts2api-panel`，本轮未替换程序、重启服务或覆盖生产凭据。没有修改 Pi 配置，也没有发布 GitHub Release。
