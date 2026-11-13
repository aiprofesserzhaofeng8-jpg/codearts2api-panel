# R2 来源能力补齐与逐项验收

日期：2026-11-07。范围：修复来源已支持但共享名称因能力字段缺失而拒绝的问题。R1 已有改动保留，R3–R6 未在本批实施。

结论：代码修复与 39 项逐项核查完成，37 项通过真实来源及共享名称验证，2 项因原始来源 404 受阻。按修复计划的完成口径，本批为部分完成；待来源恢复复验及本机部署，不能宣称全部上线完成。

## 实现与边界

- 复用现有版本化 default_models.json，版本由 4 升至 5，新增 36 个精确来源的历史验收证据，覆盖 40 个来源—能力组合。原 39 个公开名称—能力组合中 Kimi-K3 工具有两个来源，因此来源组合数多一项。每项有证据引用及日期；并非新增注册中心。
- source_capabilities.go 只为缺失的 ImageInput/ToolCalling 补充已审核证据。上游明确的否定值优先；未知型号、其他地区、未经审核的变体不猜测能力。Qoder 的两个已审核命名别名还核对显示名，显示名变化后停止补充。
- 缺失目录的占位来源保持未知，不能凭历史记录冒充当前可用来源。已有别名沿原始来源投影，公开名称、模型列表、client-profile 和能力筛选一致。源目录的原始观测保持不变。
- Trae 当前适配器只传文本，因此其目录明确给出 ImageInput=unsupported。共享模型仍需单一候选同时满足图片、工具和输出限制；未取消门禁，也不跨原生型号降级。
- 原生规格表未扩大，原生能力仍是上限。例：GLM-5 官方列出文本输入与 Function Call，来源识图成功不能提升该原生型号的图片能力：[官方规格](https://docs.z.ai/guides/llm/glm-5)。未知原生身份继续按来源服务表达，实测结果不证明底模身份。
- 没有增加持久化状态、文件仓库、每请求网络探测或管理员逐模型配置。没有猜测上下文和输出上限。

## 验证方法与结果

- 隔离测试：全部 40 个精确来源—能力组合均可通过共享候选筛选；负值、未知、变体、地区、名称变化、缺失来源与别名回归通过。36 个来源的 HTTP 模型列表/client-profile 一致性及来源移除后不再宣传能力的测试通过。
- 完整 make check 通过：前端 56 项、全 Go 测试及 vet、API 类型/架构边界、部署工具 100 项和迁移工具检查。专项 race 检查通过。
- 真实验证使用带新 catalog 代码的隔离网关，接收原公开名称、执行实际授权与能力筛选和 Chat 转换，再以原始来源 ID 转发到运行中的网关。隔离状态只含合成账号，无真实平台凭据；生产账号选择和原生平台协议仍由原网关执行。它验证新共享路由与真实平台的组合，不能替代完整新二进制部署后的复验。
- 工具必须完成“调用→合成工具结果回传→最终随机常量”；图片必须读出随机字符与蓝色方块。工具不执行任意系统命令。
- Qoder Auto 与 Fast 的首轮随机字符识别失败各追加一轮，原始失败记录保留；补测均通过。不据此保证长期视觉准确率。
- 首轮隔离配置按工具行构建 Qwen3.8-Max 时漏加 Qoder 图片来源。已修正为原始来源并集并补验，图片通过；同时请求图片与工具时，没有同一来源满足两者，仍明确返回 model_capability_unavailable。首轮测试配置错误保留在原始结果中。
- Kimi-K3 的另一原始来源 trae/cn/kimi-k3 另行完成工具闭环验证。
- Pi：在临时目录执行现有同步工具，确认 TeleAgent 共享名获得 image 输入声明、其他 provider 不变；Pi 经 OpenPangu-2.0-Pro 完成 read 工具闭环，经 TeleAgent-Chat-Flash 识别随机图片。三项通过。未修改用户 Pi 配置。
- Pi 测试使用 8192 上下文/2048 输出的临时测试窗口，仅用于短样例、不是模型规格或导出建议。39 项的隔离源均未提供完整上下文/输出推荐，因此 client_profile_status=missing_limits 仍如实保留；自动新增 Pi 型号的参数缺口属于 R6。

## 39 项逐项结论

| 公开名称 | 能力 | 原始来源 | 当前结论 |
| --- | --- | --- | --- |
| Qwen3-VL-235B | 工具 | codearts/Qwen3-VL-235B | 外部受阻：原始来源当前返回 404；未伪造共享调用通过 |
| codearts-speed | 工具 | codearts/speed | 已修复；原始来源与共享名称实测通过 |
| kimi-k2.6-vl | 工具 | codearts/kimi-k2.6-vl | 外部受阻：原始来源当前返回 404；未伪造共享调用通过 |
| openpangu-2.0-flash | 工具 | codearts/openpangu-2.0-flash | 已修复；原始来源与共享名称实测通过 |
| openpangu-2.0-pro | 工具 | codearts/openpangu-2.0-pro | 已修复；原始来源与共享名称实测通过 |
| qoder-auto | 图片 | qoder/cn/auto | 已修复；原始来源与共享名称实测通过（保留首轮识图失败） |
| qoder-fast | 图片 | qoder/cn/fast | 已修复；原始来源与共享名称实测通过（保留首轮识图失败） |
| teleagent-chat-flash | 工具 | teleagent/cn/chat-flash | 已修复；原始来源与共享名称实测通过 |
| teleagent-chat-flash | 图片 | teleagent/cn/chat-flash | 已修复；原始来源与共享名称实测通过 |
| teleagent-chat-nano | 工具 | teleagent/cn/chat-nano | 已修复；原始来源与共享名称实测通过 |
| trae-Doubao-Seed-Evolving | 工具 | trae/cn/Doubao-Seed-Evolving | 已修复；原始来源与共享名称实测通过 |
| trae-aquila | 工具 | trae/cn/aquila | 已修复；原始来源与共享名称实测通过 |
| trae-custom_model_gpt-5 | 工具 | trae/cn/custom_model_gpt-5 | 已修复；原始来源与共享名称实测通过 |
| trae-custom_model_gpt-6 | 工具 | trae/cn/custom_model_gpt-6 | 已修复；原始来源与共享名称实测通过 |
| trae-explore_sub_agent_v2 | 工具 | trae/cn/explore_sub_agent_v2 | 已修复；原始来源与共享名称实测通过 |
| trae-seed-code-pro-0430 | 工具 | trae/cn/seed-code-pro-0430 | 已修复；原始来源与共享名称实测通过 |
| trae-summary | 工具 | trae/cn/summary | 已修复；原始来源与共享名称实测通过 |
| deepseek-v4-flash-official | 工具 | trae/cn/DeepSeek-V4-Flash-Official | 已修复；原始来源与共享名称实测通过 |
| deepseek-v4-pro-official | 工具 | trae/cn/DeepSeek-V4-Pro-Official | 已修复；原始来源与共享名称实测通过 |
| deepseek-v4.1-flash-1017 | 工具 | codearts/deepseek-v4.1-flash-1017 | 已修复；原始来源与共享名称实测通过 |
| deepseek-v4.1-flash-1017 | 图片 | codearts/deepseek-v4.1-flash-1017 | 已修复；原始来源与共享名称实测通过 |
| doubao-seed-2.0-code | 工具 | trae/cn/Doubao-Seed-2.0-Code | 已修复；原始来源与共享名称实测通过 |
| doubao-seed-2.1-pro | 工具 | trae/cn/Doubao-Seed-2.1-Pro | 已修复；原始来源与共享名称实测通过 |
| doubao-seed-2.1-turbo | 工具 | trae/cn/Doubao-Seed-2.1-Turbo | 已修复；原始来源与共享名称实测通过 |
| glm-5 | 工具 | trae/cn/glm-5 | 已修复；原始来源与共享名称实测通过 |
| glm-5-turbo | 工具 | trae/cn/glm-5-turbo | 已修复；原始来源与共享名称实测通过 |
| glm-5.2-sft-harmony | 工具 | codearts/glm-5.2-sft-harmony | 已修复；原始来源与共享名称实测通过 |
| glm-5.3-flashx | 工具 | trae/cn/glm-5.3-flashx | 已修复；原始来源与共享名称实测通过 |
| kimi-k2.6 | 工具 | trae/cn/kimi-k2.6 | 已修复；原始来源与共享名称实测通过 |
| kimi-k2.7-code | 工具 | trae/cn/kimi-k2.7-code | 已修复；原始来源与共享名称实测通过 |
| kimi-k3 | 工具 | codearts/kimi-k3, trae/cn/kimi-k3 | 已修复；原始来源与共享名称实测通过 |
| kimi-k3 | 图片 | codearts/kimi-k3 | 已修复；原始来源与共享名称实测通过 |
| mimo-v2.6-flash | 工具 | trae/cn/mimo-v2.6-flash | 已修复；原始来源与共享名称实测通过 |
| minimax-m3 | 工具 | trae/cn/minimax-m3 | 已修复；原始来源与共享名称实测通过 |
| qwen-3.7-plus | 工具 | trae/cn/qwen-3.7-plus | 已修复；原始来源与共享名称实测通过 |
| qwen3.8-max | 工具 | trae/cn/qwen3.8-max | 已修复；原始来源与共享名称实测通过 |
| qwen3.8-max | 图片 | qoder/cn/qmodel_38max | 已修复；原始来源与共享名称实测通过（修正隔离来源配置后复验） |
| qwen3.8-flash | 工具 | qoder/cn/qfmodel | 已修复；原始来源与共享名称实测通过 |
| qwen3.8-flash | 图片 | qoder/cn/qfmodel | 已修复；原始来源与共享名称实测通过 |

## 产物与部署

详细原始结果、补测、配置更正、Pi 结果及检查日志保存在本机忽略目录 artifacts/r2-source-capabilities-2026-11-07/；首次结果不覆盖，最终聚合为 final-results.json 与 final-summary.json。源码修改前备份在本机仓库外的私有备份目录。凭据与运行状态不进入 Git。

本次没有替换系统服务、修改统一模型定义或分组，没有发布 Release 或推送 GitHub。运行服务仍为原来的 PID 2102403。真实测试会产生正常请求用量与日志；这与修改配置分别记录。

后续只需对两个已缺失来源在恢复后补验，并在部署新代码后复核公开接口；其他 R3–R6 的问题保持各自范围，不能由这次 Chat 验收代替。
