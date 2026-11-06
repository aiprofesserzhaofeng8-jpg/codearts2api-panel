# 原生模型能力对齐验收（2026-11-06）

## 规则

共享名称优先服从原生型号规格，再取授权来源实际支持的能力。Flash、Pro、Vision 与日期变体保留区别；不能用一次识图成功或模型自述认定底模。

目录版本 4 沿用已有 catalog，新增 13 项精确型号的官方参考；未核实的型号仍标为来源证据，不包装为已核实原生能力。没有状态迁移、第二套注册中心、自动改写来源或逐请求探测。

## 本次官方证据

| 精确型号 | 原生图片输入 | 官方依据 |
| --- | --- | --- |
| `glm-5` | 不支持 | [官方资料](https://docs.z.ai/guides/llm/glm-5) |
| `glm-5-turbo` | 不支持 | [官方资料](https://docs.z.ai/guides/llm/glm-5-turbo) |
| `glm-5.2` | 不支持 | [官方资料](https://docs.z.ai/guides/llm/glm-5.2) |
| `glm-5.3` | 不支持 | [官方资料](https://docs.z.ai/guides/llm/glm-5.3) |
| `glm-5.3-flash` | 支持 | [官方资料](https://docs.z.ai/guides/vlm/glm-5.3-flash) |
| `glm-5.3-flashx` | 支持 | [官方资料](https://docs.z.ai/guides/vlm/glm-5.3-flash) |
| `glm-5.3-flashx-legal` | 支持 | [官方资料](https://docs.z.ai/guides/vlm/glm-5.3-flash) |
| `deepseek-v4-pro` | 不支持 | [官方资料](https://huggingface.co/deepseek-ai/DeepSeek-V4-Pro) |
| `deepseek-v4-flash` | 不支持 | [官方资料](https://huggingface.co/deepseek-ai/DeepSeek-V4-Flash) |
| `deepseek-v4-flash-vision-exp` | 支持 | [官方资料](https://api-docs.deepseek.com/news/news260821/) |
| `deepseek-v4.1-flash` | 支持 | [官方资料](https://api-docs.deepseek.com/api/list-models/) |
| `deepseek-v4.1-flash-1017` | 支持 | [官方资料](https://huggingface.co/deepseek-ai/DeepSeek-V4.1-Flash-1017) |
| `hy3` | 不支持 | [官方资料](https://cloud.tencent.com/document/product/1823/132252) |

原生模型支持工具，不保证平台适配器与每种协议均已支持工具。数值上下文建议继续由已有来源策略生成，不把官方上限当成实测上下文。GLM-5.3-Flash 官方包含文件/视频能力，也不代表本项目已实现文件/视频 API。

## 已完成的实现验证

- catalog 回归：原生纯文本上限、Qoder 精确别名与目录名称漂移、Flash/日期变体、未知来源、旧分组映射、自定义公开别名、源元数据不变。
- HTTP 回归：列表和 ready/missing_limits 配置声明一致；Chat/Responses/Messages 的流式与非流式图片请求均在选号之前拒绝；完整平台服务入口单独标注来源能力。
- 拒绝原因：HTTP 400 / `native_model_capability_unsupported`，请求级终止，不切换到 Flash、不触发模型故障冷却。

## 部署与真实验证

- 本机系统服务已从 v0.4.4 升级为 v0.4.5，监听 `127.0.0.1:10087`，健康检查正常，7/7 账号就绪。公开目录仍为 89 项；其中 11 项获得本次官方规格参考，其余不是已核实的原生型号声明。
- 升级前备份：`/opt/codearts2api/backups/before-v0.4.5-1793975386296573984.tar.gz`；旧程序由维护工具保留，凭据与账本沿原升级路径维护。
- `make check` 全通过：前端 56 项、Go 全包测试/vet、静态资源与架构边界、100 项部署测试、状态导出回归；新增 catalog/server 目标用例的 race 测试通过，客户端脚本 12 项通过。
- 本机 HTTP 与 Pi 验证共 54 项通过：7 项模型列表/配置一致性、7 个纯文本型号 × 3 协议 × 2 种响应模式的 42 项图片拒绝、3 项真实生成、2 项 Pi 真实生成。
- 真实生成：共享 `glm-5.3` 文本成功；独立 `glm-5.3-flash` 与共享 `deepseek-v4.1-flash` 的随机图片文字/颜色识别成功。
- Pi 实际运行：`glm-5.3` 返回 `NATIVE_OK`；共享 `deepseek-v4.1-flash` 识别随机图片文字 `UGXK4NF` 和蓝色方块。
- Pi 已同步 71 项现有/参数齐全的条目：GLM-5.3、GLM-5.2、Hy3 等调整为 text，DeepSeek-V4.1-Flash 保留 image；模型配置先备份，默认模型及 settings 文件字节未变化。
- Pi 限制：`glm-5.3-flash` 的部分来源缺少可验证的数值推荐参数，因此未自动新增 Pi 条目；网关图片请求已通过，不能把此结果说成 Pi Flash 验收通过。未猜测来源上下文或扩大它的 token 限制。
- 私有原始结果与日志：`artifacts/native-model-alignment-v0.4.5/`。最初 Pi 验证脚本假定 Flash 条目存在而中止，前 53 项结果保留，补充 DeepSeek Pi 验证得到第 54 项；没有通过重跑覆盖失败诊断。

## 证据解释

此前 WorkBuddy `glm-5.3` 图片测试实际通过，但直连返回的 `usage.quota.model` 变为 `glm-5.3-flash`。这只能证明该平台入口提供了图片服务，不能证明原生 GLM-5.3 是视觉模型，也不能据此认证具体权重。完整证据见 [WorkBuddy 原生调用调查](WORKBUDDY-GLM53-NATIVE-2026-11-06.md)。

当前修复将共享 `glm-5.3` 声明为 text，独立 `glm-5.3-flash` 在来源支持时保留 image。完整 `workbuddy/cn/glm-5.3` 仍代表该平台服务，并同时给出官方纯文本规格参考。

已有全目录能力批测是修复前的历史行为快照，不能把其识图 PASS 重新解释成原生能力证明。本次不重复全目录付费批测，也没有认证所有未知或平台内部型号的身份。
