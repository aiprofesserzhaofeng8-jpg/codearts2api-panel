# Pi / 码道 DeepSeek 压缩适配

2026-10-26，压缩失败请求的输入约 12K–15K Token，而输出连续达到 8192 Token；部分预算用于思考，正文摘要也很长。Pi 正确地拒绝把 finish_reason=length 的不完整摘要保存为检查点。

本机扩展：`/home/white/.pi/agent/extensions/codearts-compaction.ts`，源码副本在 `scripts/pi/codearts-compaction.ts`。

仅匹配 codearts-local/deepseek-v4.1-flash 的 Chat Completions 请求，以及 Pi 内置摘要专用 system 提示。匹配时设 thinking.type=disabled，删除 reasoning_effort，并增加精简摘要约束；不改普通编程请求、不更换模型、不伪造 finish_reason、不接受被截断的摘要。

实际读取失败会话的副本，调用 Pi 原生 compact() 和真实码道上游验证通过：输入 21,241 Token、输出 5,522 Token、推理 0 Token，完整返回摘要。未修改原会话。脱敏结果在 artifacts/pi-compaction-live-check.json。

使用：先停止当前自动重试，执行 `/reload` 载入扩展，再执行 `/compact`。如果启动时传了 `--no-extensions`，需显式加 `-e /home/white/.pi/agent/extensions/codearts-compaction.ts` 或正常启用扩展。

此适配依赖 Pi 的摘要提示标识；Pi 升级或切换 Responses 协议后需要重新核对。它降低本次反复耗尽输出预算的风险，不保证任意体量的历史永远能一次压缩。不要关闭 Pi 对不完整摘要的检查。

后续已迁入中转服务端：新客户端不再需要复制此扩展。使用 scripts/connect-client.py pi 即可自动获取压缩参数；旧扩展保留时服务端会避免重复附加摘要指令。详见 docs/CLIENTS.md。

2026-10-26 后续质量验收发现，突发回填会使 Pi 将摘要输出预算压到 1。新增受控分段摘要恢复，启动器内置并自动安装，源码仍为 scripts/pi/codearts-compaction.ts。更新后的原失败场景与渐进场景各三轮真实验证通过。详细记录见 ../acceptance/REMEDIATION-2026-10-26.md。上文“无需扩展”仅适用于标准服务端摘要适配，不覆盖这个新的客户端恢复场景。
