# WorkBuddy 搜索、提示词与出站清洗

这三项只作用于 WorkBuddy，参考本机 `workbuddy2api-panel` 的搜索协议、提示词替换和指纹清洗实现；来源许可为 MIT，见 `LICENSE.workbuddy`。码道与 Trae 的请求不应用这组设置；它们有自己的提示词/清洗配置及独立搜索后端，见 [平台能力与维护边界](PROVIDER-FEATURES.md)。

## 配置

面板「配置 → WorkBuddy」保存后对新请求生效，也可通过管理接口更新 `workbuddy` 对象：

```json
{
  "workbuddy": {
    "prompt_mode": "custom",
    "prompt_text": "",
    "prompt_fallback": true,
    "sanitize": true,
    "search_enabled": true,
    "search_scope": "cn"
  }
}
```

这些也是新安装及旧状态文件缺失该配置时的默认值。各布尔开关保存为 false 后重启仍保留。提示词上限为 64 KiB UTF-8。管理接口支持部分更新，未提交字段保持原值。

- `custom`：删除出站 messages 中的 system/developer 消息，插入网关提示词；留空使用内置工程助手提示词。
- `passthrough`：保留客户端系统提示词。独立开启指纹清洗时，匹配的文本仍会被清洗；完全保留原文需同时关闭清洗。
- `prompt_fallback`：仅在 passthrough 模式、没有任何响应分片被消费、上游明确返回内容策略拦截时，换中性提示词重试一次。重试保持同一账号、模型、并发租约及请求超时，继续检查预算；不影响其它请求，不设置全局降级期。通道校验失败 `11128 request illegal`、认证失败、一般参数错误不会被当成内容误报重试。第二次失败直接返回，不通过换账号重复生成。
- 已识别的摘要请求保留摘要专用 system，不应用 custom 替换或中性降级；清洗开关仍生效。
- `sanitize`：清洗消息文本及历史推理文本中参考项目列举的客户端固定模板、计费头片段、`cc_...` 标识及错误码字符串。工具参数作为 JSON 解析后只改字符串值；工具调用 ID、字段名、数字、工具定义和图片地址保持不变。它不是通用密码或个人信息脱敏器，也不保证消除上游拒绝。

## 联网搜索

客户端搜索 base URL 设置为 `http://127.0.0.1:10087/v1/search`，调用密钥与其它 `/v1/*` 接口相同。端点为 `POST /v1/search/messages`：

```json
{
  "model": "workbuddy/cn/web-search",
  "stream": false,
  "max_results": 5,
  "tools": [{"type": "web_search_20250305", "name": "web_search"}],
  "messages": [{"role": "user", "content": "Perform a web search for the query: Go context cancellation"}]
}
```

仅抽取最后一条有文本的 user 消息；支持字符串和 text block 数组，剥除上面的搜索指令前缀。搜索词限制 2–4096 个字符，结果默认 5 条、最多 10 条。搜索返回完整 Messages JSON，包含 `web_search_tool_result`；摘要位于 text block 的 `citations[].cited_text`。这不是调用模型生成带引用回答，也不接受 `stream:true`。

通用入口未指定 model 或使用无斜线标签时，客户端 model 只作为响应标签，按 WorkBuddy `search_scope` 选择账号地区。带其他平台前缀的请求会明确路由或报错，不会转到 WorkBuddy。也可使用专用 base URL `/v1/search/workbuddy`。使用 `workbuddy/cn/...` 或 `workbuddy/global/...` 可明确指定地区。搜索不会受到对话默认模型或强制模型改写，不跨地区回落。请求头 `X-Conversation-ID`、`X-Conversation-Request-ID`、`X-Trace-ID` 可传入现有会话标识。

搜索复用 API Key 鉴权、限流、账号并发租约、凭据续期、失败换号及冷却。整个搜索含重试最多 20 秒，也受更短的全局请求超时限制。日志以 `workbuddy/<地区>/web-search`、协议 `search` 和用途 `search` 标识。搜索限流冷却该账号的搜索能力，凭据故障按现有账号级规则处理。

分组密钥只能使用组内账号。启用模型白名单时，须在「账号分组 → 编辑」中额外勾选对应地区的「WorkBuddy 联网搜索」。搜索能力不作为可对话模型加入 `/v1/models`。

上游搜索没有 Token 或费用回执：协议里的 usage 为兼容字段 0，内部日志保留未知用量／未知费用，不解释成免费。请求次数预算照常计数；配置了 Token 或 USD 预算的密钥会在调用前拒绝搜索，避免绕过无法核算的预算。请为搜索使用仅设置请求次数预算的独立密钥。

## 验证边界

自动化使用本地模拟上游验证 CN/global 请求头、扁平结果、引用、空结果、错误、限流换号、凭据续期、分组白名单、共享并发、取消释放、参数与预算拦截、提示词重试边界、配置落盘和页面交互。没有以真实账号调用搜索或模型；实际可用性、活动权限和扣费以上游为准。
