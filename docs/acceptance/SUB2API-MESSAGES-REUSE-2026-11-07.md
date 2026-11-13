# Sub2API Messages 消息转换核心复用

日期：2026-11-07。范围：将现有 Messages→Chat 的消息转换接到固定版本的 Sub2API 转换函数，保留 R1/R2/R3 和型号身份修复的边界。没有接入 Sub2API 的账号、数据库、调度或服务层，也没有宣布所有平台新增原生 Messages 端点。

## 实际复用的代码

新增 [anthropic_chat_messages.go](../../backend/internal/responsescompat/anthropic_chat_messages.go)，从 Sub2API `6b4b37db43443b5e88e424fe3c69602cc3810dfb` 提取七个函数：

- `chatcompletions_anthropic_bridge.go` 的消息分发、用户消息展开、助手工具调用、thinking→reasoning_content 转换。
- `anthropic_to_responses.go` 的图片转换、工具结果内容提取、文本块提取。

这部分采用 LGPL-3.0，继续放在项目已有的 LGPL 兼容库。根 NOTICE、README、[UPSTREAM.md](../../backend/internal/responsescompat/UPSTREAM.md) 已更新；[messages-upstream.json](../../backend/internal/responsescompat/messages-upstream.json) 保存原始文件哈希、版本及函数清单。原有 Responses 转换文件没有整体追随这次新版本升级。

[protocol/anthropic.go](../../backend/internal/protocol/anthropic.go) 的消息展开调用该库；Sub2API 的函数实际参与生产请求路径，不是仅下载参考。纯文本字符串使用原有快速路径，普通 JSON 消息调用共享转换核心。工具调用参数以 RawMessage/字符串保留，避免经过 float64 损失大整数。

## 必要的本地适配

直接采用 Sub2API 整份请求桥会引入不同的系统提示词过滤、输出预算下限、推理默认值和工具过滤行为。本批只复用消息核心，顶层参数、system 处理及响应/SSE 实现继续沿用已验证的项目契约。

| 内容 | 本项目保留的契约 |
| --- | --- |
| 普通思考 | 带工具和不带工具的普通推理历史均保留；多个块按原字符顺序连接，避免额外插入换行改变内容 |
| 签名及加密思考 | 校验后保留原生历史侧字段，不因使用 Chat 转换桥而删除签名或伪装为普通推理 |
| 文本及助手图片 | 保留助手文本块边界和图片，不采用助手转换只提取文本的行为 |
| 工具结果 | 保留原始文本及合法空结果，不将空结果改为 `(empty)`；现有非字符串结果投影继续适用 |
| 工具结果图片 | 支持原有 base64 与 URL；提取后仍带原工具调用 ID 的归属标记，平行工具结果保持相邻 |
| 未支持内容 | 文件、文档等仍在转换/上游调用前明确拒绝；不使用上游库忽略未知内容的行为绕过 R1 |
| 预算及控制参数 | 不套用上游的默认推理强度、最小输出预算或自动采样参数清理；已有平台契约继续验证 |

架构检查仅允许 `protocol → responsescompat` 这一条纯转换依赖，同时验证兼容库及间接依赖没有 HTTP、账号、存储等业务依赖；反向依赖仍禁止。测试明确检查合法复用及兼容库引入 HTTP 时仍被拒绝。没有通过关闭架构检查来绕过限制。

## 验证与性能

完整 make check、协议与兼容库测试、专项 race 检查覆盖 R3 普通/签名历史、合法拒答、并行工具、大整数、图片及提示词默认值。新增兼容库用例直接验证提取函数的工具调用、推理、工具图片归属和空结果行为，原 R3 回归保持。

固定输入的纯转换微基准（linux/amd64、i5-13400F、GOMAXPROCS=2，前后各两次）：

| 样本 | 修改前 | 修改后 | 分配变化 |
| --- | --- | --- | --- |
| 1 KiB 历史 | 约 9.1–9.2 μs | 约 12.0–12.4 μs | 约 7.6 KB/106 次 → 9.7 KB/131 次 |
| 64 KiB 历史 | 约 69.1–69.7 μs | 约 70.2–70.9 μs | 约 72.2 KB/106 次 → 74.2 KB/131 次 |

保留输入契约的适配有额外解析和小额分配成本；不能声称直接复用提高了性能。这只是转换函数样本，不是服务吞吐量、总内存或真实延迟压测。没有新增每请求网络探测、后台任务、持久化内容、等待队列或模型路由缓存。

真实验收使用新源码隔离网关，通过原有生产 Chat 接口调用已授权账号，固定到与 R3 相同的来源。共享名称 7 个 × 流式/非流式两轮工具闭环；原始来源和 Pi 分别复核。首次 R3 结果保留在原目录，本轮结果写入 artifacts/sub2api-messages-reuse-2026-11-07/，不覆盖历史证据。

| 公开名称 | 固定原始来源 | 共享非流式 | 共享流式 | 原始来源非流式 |
| --- | --- | --- | --- | --- |
| workbuddy-hy3 | workbuddy/cn/hy3 | PASS | PASS | PASS |
| workbuddy-hy3-x | workbuddy/cn/hy3-x | PASS | PASS | PASS |
| workbuddy-hy4free | workbuddy/cn/hy4free | PASS | PASS | PASS |
| workbuddy-hy4std | workbuddy/cn/hy4std | PASS | PASS | PASS |
| kimi-k2.8-preview | qoder/cn/kmodel | PASS | PASS | PASS |
| kimi-k3-1 | workbuddy/cn/kimi-k3-1 | PASS | PASS | PASS |
| kimi-k3.1-pro | qoder/cn/kmodel_latest | PASS | PASS | PASS |

共享 14/14、原始来源 7/7、Pi Messages read 工具 1/1 通过。21 项 HTTP 工具闭环均实际包含普通思考历史，第二轮消费随机结果后正常结束；无重测或受阻项。Pi 使用临时配置，共计 44 次真实转发，生产账号、模型配置和真实 Pi 配置未被改写。临时网关已停止，其临时密钥文件已删除。

本轮日志：/tmp/sub2api-messages-check-final.log、/tmp/sub2api-messages-unit.log、/tmp/sub2api-messages-race.log、/tmp/sub2api-messages-bench-before.log、/tmp/sub2api-messages-bench-after.log。临时测试仅执行合成工具或 Pi 的 read 工具，不执行任意模型生成命令；真实生成仍按原有网关规则产生正常用量。

## 部署与回退

修改前源码快照：/home/white/backups/codearts2api/before-sub2api-messages-20261107-185422/source-changes.tar.gz，另存 Git HEAD；不含生产账号状态。

本轮仅完成本地源码改造和验收。没有推送 GitHub、发布 Release、升级或重启 codearts2api.service。生产仍使用原 PID 2102403，不能把隔离网关成功理解为在线程序已升级。
