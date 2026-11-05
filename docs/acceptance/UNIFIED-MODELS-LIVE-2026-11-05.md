# 全局统一模型：首批真实平台验证

日期：2026-11-05。范围：WorkBuddy CN 与 Trae CN / SOLO 的 `deepseek-v4.1-flash`，验证 U1–U6 的统一名称路由在真实上游上的行为。结论：双平台三协议文本及工具闭环已通过，并修复实际发现的工具能力元数据缺失。

这是 [U6 隔离验收](UNIFIED-MODELS-U6-2026-11-05.md) 之后的真实验证；U6 当时的“真实跨平台未验证”和本地包摘要保留其历史含义。本轮源码修复尚未部署或发布，原 U6 候选包也没有被覆盖。

## 环境与控制

- 使用当前工作区的真实 app.Build、Store、HTTP Handler 和 WorkBuddy/Trae 原生适配器，直接请求平台正式端点。不是模拟上游，也没有用旧系统服务转发测试流量。
- 现有系统服务位于 `127.0.0.1:10087`，由 `codearts2api.service` 管理。其数据受系统权限保护；未读取受保护数据或取得管理会话，未升级、重启或修改该服务的账号、密钥、路由设置。鉴权检查可能留下原服务的失败审计记录。
- 本地可读的 schema 10 快照包含两份 WorkBuddy 和一份 Trae 凭据；均先检查身份/地区和到期时间，再以真实目录与额度接口确认仍可使用。推理各选一个账号，不将账号名称、UID、凭据或原始响应写入公开记录。
- 只读取源快照字节，不对源调用 Store.Open/Save。新状态、测试密钥、统一模型、临时工具文件全部位于测试临时目录。结束后由测试框架清理，并检查源文件未变化。
- 关闭续期、IDE 回写、领奖和后台运行循环；适配器 Refresh 额外拒绝调用。本轮刷新尝试为 **0**、领奖为 **0**。每次生成输出上限 128 Token，单次总超时 35 秒，不自动重试。本轮总计 **24 次真实生成**，没有无限重试或全目录付费扫描。

目录中的两个准确 ID 均为 `deepseek-v4.1-flash`，显示名分别为 Deepseek-V4.1-Flash / DeepSeek-V4.1-Flash。对应关系依据供应商目录，而非询问模型“你是谁”；本轮不能独立证明平台背后的权重或部署完全相同。

## 结果

| 验证 | WorkBuddy | Trae |
| --- | --- | --- |
| 实际目录和额度接口 | 成功 | 成功 |
| 公开名的模型列表和 client-profile | 通过统一目录；两来源保守合并 | 相同公开定义；沿用已有 Trae 保守策略 |
| Chat Completions，非流式 / SSE | 2/2 通过 | 2/2 通过 |
| Responses，非流式 / SSE | 2/2 通过 | 2/2 通过 |
| Messages，非流式 / SSE | 2/2 通过 | 2/2 通过 |
| 原始型号工具调用 → 读取临时文件 → 返回结果 | 通过 | 通过 |
| 统一名称工具调用 → 读取临时文件 → 返回结果 | 修复后通过 | 修复后通过 |

12 次文本调用都返回了请求标记、正常终止及 usage，响应保持同一个公开名称，日志中的实际平台和完整原始型号正确；每次只有一次上游尝试，结束后租约为零。通过在隔离状态中暂停另一平台，分别强制每个真实来源执行，未将只命中一个来源当成双平台通过。

工具首轮真实验证中，原始型号两组闭环通过（4 次生成），统一名称在网关返回 400（零上游调用）。修复后重新验证原始与统一名称，共 4 组闭环全部通过（8 次生成）。合计：文本 12 + 首轮工具 4 + 修复后工具 8 = **24 次真实生成**。工具仅执行预设 `probe.txt` 的读取，不执行模型返回的任意文件路径或命令。

两平台额度均有可用读数。前后余额仅作为私有观察值保留，不把短时读数未变化宣称为免费，也不能排除其他客户端活动或上游记账延迟。实际账号额度和原始状态不进入本记录。

## 实际发现与修复

统一模型的能力筛选本身按规则拒绝未知工具能力。缺口在目录元数据：WorkBuddy 和 Trae 的原始工具调用可用，但生成的 provider.Model.ToolCalling 均为空，导致统一名称无法选到任何有明确工具能力的来源。

- [WorkBuddy](../../backend/internal/workbuddy/client.go) 现在读取真实 `/v3/config` 的 `supportsToolCall`：true=支持、false=不支持、缺失=未知。没有按模型名推断，也没有把所有型号默认设为支持。
- [Trae](../../backend/internal/trae/client.go) 的 `get_detail_param` 没有明确工具标记。仅对本轮完整工具闭环已通过的精确配置 `deepseek-v4.1-flash` 补充支持；其他 ID、大小写不同的 ID 和 custom_model_no-fc 保持未知。没有把 display_config.multimodal、reasoning_model 等字段当作工具能力依据。
- 补充两项离线回归，覆盖 WorkBuddy true/false/缺失，以及 Trae 精确已验证 ID / 未验证 ID / 大小写差异。没有扩展 Trae 图片适配或虚构上下文上限。

## 可重复验证

[真实验证用例](../../backend/internal/app/unified_live_test.go) 默认跳过，只有显式环境变量才访问真实平台。普通 make check 不发送真实请求。直接 Go 命令前先构建前端；以下输出目录须保持私有，不将实际源状态或日志发到公开仓库：

```bash
make frontend
cd backend
CODEARTS_UNIFIED_LIVE_STATE=/absolute/path/to/readable-snapshot.json \
CODEARTS_UNIFIED_LIVE_REPORT=/private/output/discovery.json \
GOMAXPROCS=2 GOFLAGS=-p=2 go test ./internal/app \
  -run '^TestUnifiedLiveDiscovery$' -v -count=1 -timeout=3m
```

上面的 Go 命令在 `backend/` 执行，使用 `backend/go.mod` 锁定工具链。目录/额度发现不做生成；需要真实推理时再显式加 `CODEARTS_UNIFIED_LIVE_INFERENCE=1`，分别运行 `TestUnifiedLiveProtocols`（12 次生成）和 `TestUnifiedLiveTools`（8 次生成）。各次测试有总尝试保护；不要把默认跳过误判为真实通过。

报告路径必须是新的文件：发现已存在文件时，在网络访问前退出；写入使用排他创建，不覆盖源状态、旧报告或链接目标。追加了本地回归验证这一保护，未为测试报告保护重复消费真实推理额度。

本轮还通过提供者/目录/应用回归、对应竞态检查、全套 make check 和独立构建。最终命令结果及脱敏摘要见 [机器记录](UNIFIED-MODELS-LIVE-CHECKS-2026-11-05.json)。私有原始发现结果位于本地 `artifacts/unified-models-live-2026-11-05/`，已被 Git 忽略。

## 尚未验证

本轮只验证这两个平台的这一型号，不代表所有型号通过。未验证其他平台、图片/音频、长上下文极限、真实自然故障触发的自动切换、真实上游 Pi 会话；其中协议恢复、权限变化、取消等仍以 U6 隔离验收为证据。

旧快照的 CodeArts 凭据已过期，未用它触发续期；其他平台在本轮可读快照中无可用凭据。系统服务当前全部账号的验证，需要另外通过正常登录取得访问权限。当前代码修改和构建未推送 GitHub、未发布，也未替换正在运行的服务。
