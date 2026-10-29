# 各平台的提示词、清洗与联网搜索

配置按 `codearts`、`workbuddy`、`trae` 三个对象独立保存，控制台「配置」也分为三个区块。保存后对新请求生效。

| 能力 | CodeArts | WorkBuddy | Trae CN |
|---|---|---|---|
| custom / passthrough 系统提示词 | 支持，默认 passthrough | 支持，默认 custom | 支持，默认 passthrough |
| 明确内容策略错误后中性提示词重试 | 支持，默认开启 | 支持，默认开启 | 支持，默认开启 |
| 已知客户端文本指纹清洗 | 支持，默认开启 | 支持，默认开启 | 支持，默认开启 |
| 搜索后端 | 独立 SearXNG，配置后启用 | 原生 `/agenttool/v1/search` | 独立 SearXNG，配置后启用 |
| 默认搜索状态 | 关闭，未配置搜索地址 | 开启 | 关闭，未配置搜索地址 |

**CodeArts 和 Trae 的原生搜索没有接入。** 本次检查了本机 CodeArts Agent 26.10.0 客户端内核：其 `websearchTool` 使用 `knowledgeBaseUrl + /api/v1/websearch`，并依赖网络区域、服务开关、场景码及另一套用户身份信息；不能据此把当前 AK/SK 推理凭据发送过去。Trae 的参考仓库没有给出可核实的独立搜索协议。因此这两个平台明确使用管理员配置的外部搜索服务，不转发到 WorkBuddy，也不把普通模型回答当成搜索结果。

## 提示词与清洗

三个入口 `/v1/chat/completions`、`/v1/responses`、`/v1/messages` 都在协议转换后、发往各平台前应用同一套请求策略，但读取各平台自己的配置。

- `prompt_mode`：`custom` 替换全部 system/developer；`passthrough` 保留客户端提示词。
- `prompt_text`：custom 模式使用的提示词；留空采用内置工程助手提示词，上限 64 KiB UTF-8。
- `prompt_fallback`：仅 passthrough、没有消费任何分片、上游明确返回内容策略错误时，换中性提示词重试一次。保持账号、凭据、模型、租约和总超时，重新检查预算并记录尝试次数。第二次失败终止；认证、权限、设备、通道和普通参数错误不触发提示词重试。
- 已识别的摘要请求保持摘要提示词，不执行 custom 替换或中性提示词重试；已有摘要策略照常工作。
- `sanitize`：仅处理 system/developer 的 content 中两条明确客户端身份模板，以及独立的 `x-anthropic-billing-header:` 计费头行。用户消息、工具结果、历史工具参数及推理内容保持原样；不再删除裸 `cc_...` 赋值或改写 `11128`。透传模式没有实际修改时返回原请求字节，避免无谓重新编码。
- 清洗属于客户端模板兼容处理，不是通用密码、凭据或个人信息脱敏器；也不保证上游一定放行。需要完全原样发送时选择 passthrough 并关闭 sanitize。

明确错误标记是保守识别规则；不能证明每个平台线上都会返回这些标记。没有匹配到的未知错误不擅自猜测、不强制重试。

## 配置 CodeArts / Trae 搜索

需要一台启用了 JSON 输出的 SearXNG 服务。可以是本机、内网或自己管理的 HTTPS 服务。没有预置公共搜索站点。依据 [SearXNG 官方 Search API](https://docs.searxng.org/dev/search_api.html)，本项目发送 POST 表单 `q=...&format=json&categories=general`；服务需要在 `settings.yml` 的 `search.formats` 中启用 `json`。

在对应平台区块填写服务地址，例如 `http://127.0.0.1:8080/search`，勾选「启用此平台的联网搜索」并保存。CodeArts 与 Trae 地址分别保存，可以使用不同服务；管理接口部分更新示例：

```json
{
  "codearts": {
    "search_backend": "searxng",
    "search_url": "http://127.0.0.1:8080/search",
    "search_enabled": true
  },
  "trae": {
    "search_backend": "searxng",
    "search_url": "http://127.0.0.1:8081/search",
    "search_enabled": true
  }
}
```

URL 不接受用户密码、查询参数或片段，不支持通过地址内嵌 Token。根地址自动使用 `/search`，带路径的地址按该路径请求。外部服务调用不携带 API Key、模型账号凭据、设备信息或客户端追踪头，不跟随重定向。

## 客户端搜索入口

建议使用平台专用 base URL；只会使用指定平台的配置、账号范围及搜索权限。

| 平台 | 搜索 base URL | 记录及分组权限标识 |
|---|---|---|
| CodeArts | `http://127.0.0.1:10087/v1/search/codearts` | `codearts/web-search` |
| WorkBuddy | `http://127.0.0.1:10087/v1/search/workbuddy` | `workbuddy/cn/web-search` 或 `workbuddy/global/web-search` |
| Trae | `http://127.0.0.1:10087/v1/search/trae` | `trae/cn/web-search` |

客户端追加 `/messages` 发请求。专用入口允许普通 model 标签；如果 model 带平台前缀且与入口冲突，返回 400。通用 `POST /v1/search/messages` 也支持上表的明确标识。为兼容原有 WorkBuddy 客户端，通用入口的空 model 或无斜线标签仍使用 WorkBuddy 的默认地区；其它未知平台前缀返回 400，不静默跨平台回落。

例如请求 `POST /v1/search/codearts/messages`：

```json
{
  "model": "codearts/web-search",
  "stream": false,
  "max_results": 5,
  "tools": [{"name": "web_search", "type": "web_search_20250305"}],
  "messages": [{"role": "user", "content": "Perform a web search for the query: Go context cancellation"}]
}
```

只提取最后一条有文本的 user 消息，剥除示例前缀；2–4096 字符，默认 5 条、最多 10 条。返回完整 Messages JSON，包含 `web_search_tool_result` 和引用摘要。空结果数组有效；非 JSON、缺失 results、超过 1 MiB 的响应报错，不伪装成无结果。仅返回 HTTP(S) 引用，不替客户端抓取网页内容。

搜索是独立接口，不会在普通聊天请求中自动执行模型发出的 `web_search` tool call。客户端仍需配置其搜索 provider 或自行执行工具。

### 分组、并发与费用

所有入口使用网关鉴权、限流、预算及分组权限。分组白名单应额外勾选对应搜索能力；只提供所选平台和地区的能力，不把搜索能力放入可对话的 `/v1/models` 清单。外部搜索仍要求组内有可调度的对应平台账号，以统一归属和并发限制。

- WorkBuddy 原生搜索：复用凭据刷新、账号重试及冷却。
- 独立搜索：复用账号并发租约，但不刷新模型凭据，不因外部失败换模型账号，也不修改模型账号的失败次数、健康或冷却状态；成功也不能证明模型账号凭据正常。
- 取消会释放租约；整个搜索最多 20 秒，也受更短的全局请求超时限制。
- 日志记录真实目标平台、搜索能力、`search` 协议与尝试次数。上游没有 Token/费用回执，内部保留未知值；协议 usage 的 0 只是兼容字段，不能解读为免费。仅请求次数预算的密钥可用于搜索，设有 Token/USD 预算的密钥在调用前拒绝。

## 维护边界

| 模块 | 职责 |
|---|---|
| `internal/store/provider_settings.go` | 三个平台独立配置、默认值及校验；旧 WorkBuddy 配置兼容 |
| `internal/requestpolicy/` | 纯请求变换、共享提示词、仅系统提示词适用的清洗规则；不读取账号或发网络请求 |
| `internal/server/request_policy.go` | 统一应用策略、摘要豁免、一次重试和预算/尝试记录 |
| `internal/codearts/content_error.go`、`internal/trae/client.go`、`internal/workbuddy/client.go` | 各自解释原生错误，避免把设备或认证错误当成内容拦截 |
| `internal/server/search.go` | 搜索协议、明确路由、分组/预算及并发边界 |
| `internal/workbuddy/search.go` | WorkBuddy 原生搜索 |
| `internal/websearch/searxng.go` | 外部搜索传输，不依赖任何模型凭据 |

若将来取得 CodeArts/Trae 的原生搜索协议，应在对应适配器添加实现并增加显式 backend 配置及测试，不让外部后端借用模型 Token，也不在搜索处理器猜测原生 URL。

自动化验证使用模拟上游，覆盖三个聊天协议、流式/非流式、策略隔离、重试边界、摘要、搜索凭据隔离、分组、并发、取消、预算、配置迁移和浏览器操作。本次未使用真实账号进行模型或搜索调用；CodeArts/Trae 原生搜索及尚未配置的真实 SearXNG 服务均未做线上验收。

## 第一批保真修复（2026-10-29，固定版本之后）

当前聚合器增加 `refusal` 增量保留，并按 choice index 分别累积正文、推理、工具参数及结束原因。同一 tool index 可以在不同 choice 中重复，不再串接候选回答。`length` 和未知结束原因保留；usage-only 帧仍不产生额外 choice。该改动仅涉及流转非流的 Chat 聚合，不增加普通流式等待，不调整重试或提示词默认模式。

这是明确边界的小范围修复，不声明已实现任意扩展字段或所有跨协议内容的无损转换。当前聚合器仍采用显式字段结构；新增上游字段需要单独增加映射与回归，不猜测未知增量的拼接语义。宽范围清洗已移除，没有新增恢复宽范围清洗的开关；如上游因为业务文本拒绝，返回原有错误处理结果，不再次自动改写业务内容。

参考 Sub2API 固定提交 `a3298b46ebe239fb0720b068a1eb3383955bdac2` 的原始 payload 保留及逐事件刷新原则，本次未引入其运行依赖或整个转发框架。
