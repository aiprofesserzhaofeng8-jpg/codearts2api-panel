# LiteLLM 文本模型参考价

数据来自 [LiteLLM 固定版本的价格表](https://github.com/BerriAI/litellm/blob/ac7674625ceed298d6c411cc964f23aded63989d/model_prices_and_context_window.json)，适用 [MIT 许可](LICENSE)。运行 `python3 generate.py` 可重新生成随程序打包的 `prices.json`。仅此维护命令联网。

匹配范围由生成器中的产品映射决定。DeepSeek 国内按量 API、Gemini API 和 OpenCode Go 使用各自来源的原生 ID。其他接入方式由管理员设置本站费率。只取标准文本的未缓存输入、输出、缓存读三个 USD/token 字段，乘以一百万后入表。三项须完整。存在按上下文长度变化的基础价时跳过。缓存写入和批处理等不同服务档位的价格不进入这三项。
