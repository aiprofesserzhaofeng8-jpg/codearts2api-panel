# WorkBuddy GLM-5.3 原生模型字段核对

2026-11-06，国内现有账号。7 次生成直接访问 WorkBuddy `/v2/chat/completions`，读取本地适配器改写之前的原生 SSE；另有 2 次网关对照。测试脚本只读凭据，未修改生产代码、统一模型定义或 Pi 配置。直连调用使用上游额度，不进入本地网关账本。

| 发送的 model | 输入 | 原生响应 model | 原生 usage.quota.model |
| --- | --- | --- | --- |
| glm-5.3 | 纯文本 | glm-5.3 | glm-5.3 |
| glm-5.3 | 随机文字图一 | glm-5.3 | glm-5.3-flash |
| glm-5.3 | 随机文字图二 | glm-5.3 | glm-5.3-flash |
| glm-5.3 | 无文字的形状和颜色图 | glm-5.3 | glm-5.3-flash |

三次 GLM-5.3 图像题全部正确。直接指定 GLM-5.3-Flash 的两次图像对照也全部正确，但没有返回 quota 对象。模型自述未用于识别底模。

WorkBuddy 原生目录 `/v3/config` 本身把 glm-5.3 的 supportsImages 标为 true；该标记没有由本地虚构。本地请求适配器也没有把 glm-5.3 改成 Flash。它会规范化返回的顶层 model，因此此前只看网关响应名称无法观察内部处理。

**已确认的是图像请求的配额归属切换到了 Flash。** 这提示平台可能按图片输入切换处理路径，但配额字段不能单独证明全部实际计算都使用 Flash，也不能证明平台是无意分错型号。完整内部执行路径仍待平台说明或额外证据。

智谱官方 [GLM-5.3 文档](https://docs.z.ai/guides/llm/glm-5.3) 明确其为纯文本输入；[Flash/FlashX](https://docs.z.ai/guides/vlm/glm-5.3-flash) 原生支持多模态。[WorkBuddy 官方模型页](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Model) 则给其 GLM-5.3 服务标注了图片输入。因此，平台服务的图片能力与标准底模的原生能力应分开记录，不能只按相同名字推定完全等价。

同图网关对照：CodeArts GLM-5.3 被上游以 not multimodal 拒绝；CodeArts Qwen3-VL 正确识别。私有原始回执、图片及报告位于 `artifacts/workbuddy-glm53-native/`，未包含认证头或令牌，也未自动调整生产映射。
