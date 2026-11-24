# TeleAgent 与 MobileClaw 联网搜索

在「配置」中开启 TeleAgent 或 MobileClaw 的联网搜索，保存后即可通过下列接口查询网页。搜索使用账号池中对应平台的登录凭据，无需另填搜索 API Key 或部署 SearXNG。

| 平台 | 请求入口 | 搜索标识 |
|---|---|---|
| TeleAgent | `POST /v1/search/teleagent/messages` | `teleagent/cn/web-search` |
| MobileClaw | `POST /v1/search/mobileclaw/messages` | `mobileclaw/cn/web-search` |

也可使用 `POST /v1/search/messages`，通过 `model` 填入完整搜索标识选择平台。不指定平台的旧入口仍使用 WorkBuddy。

## 调用示例

密钥必须绑定有效分组，分组包含对应平台账号。如果分组开启原始型号白名单，还须在分组编辑器中允许相应的搜索标识。两个新平台的搜索开关默认关闭。

```bash
curl 'http://127.0.0.1:10087/v1/search/teleagent/messages' \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{
    "model": "teleagent/cn/web-search",
    "stream": false,
    "max_results": 5,
    "tools": [{"type": "web_search_20250305", "name": "web_search"}],
    "messages": [{"role": "user", "content": "Attention Is All You Need arxiv"}]
  }'
```

使用 MobileClaw 时替换路径中的 `teleagent` 和 `model` 中的平台名。

接口返回完整 Messages JSON：`web_search_tool_result` 包含网页标题和地址，`text.citations` 包含摘要。`max_results` 默认 5，最多 20；搜索词为 2–4096 个字符。不支持流式返回。

这是独立搜索工具入口。Pi 等客户端仍需注册调用此入口的工具；普通聊天请求不会自动搜索，也不会因模型同名而调用其他平台搜索。

## 执行与计量

- 复用现有密钥认证、分组权限、账号并发槽位和搜索请求记录。
- 原生接口错误直接进入既有搜索错误处理，不回退到 DuckDuckGo、Exa、SearXNG 或其他平台账号。
- 搜索不提供可核算的 Token/费用回执，记录为未知；响应中的兼容 usage 零值不表示免费。现有搜索预算限制保持不变。
- MobileClaw 凭据续期沿用现有账号协调器；搜索适配器不自行刷新或写入凭据。

## 验证依据

2026-11-24 使用用户授权凭据直接验证官方接口：TeleAgent 返回 HTTP 200、`code=0` 和 20 条引用；MobileClaw 返回 HTTP 200、`errno=0` 和 3 条结果，其中包含 arXiv 原文。以上是原生协议验证。

网关集成使用隔离账号、测试密钥和模拟上游验证：两个入口、请求格式、结果及摘要、结果数量限制、非法 URL 过滤、鉴权、分组拒绝、开关、错误响应和租约释放。未将真实账号凭据写入测试文件。
