# CodeArts / Trae 原生搜索调查（2026-11-24）

结论：不能把这两个平台标为“没有搜索工具”。CodeArts 本机客户端包含完整的关键词网页搜索实现；Trae 官方文档明确列出内置联网搜索。当前网关两者均仍使用外部 SearXNG，没有据此改动生产搜索路径。

## CodeArts：已找到协议，账号可用性待验证

检查来源：本机官方扩展 `huaweicloud.codearts-snap/out/extension.js`、其 `product.json`，以及 `~/.codeartsdoer/CodeArts_Agent/AgentKernel_Vscode_26_10_0_1_0_0-1159287104046016`。仅检查程序内容，没有复制客户端代码或提取登录凭据。

### 厂商搜索实现

- 工具名 `websearch`，与 `webfetch` 分开；参数 `query`，最多 1000 字符。
- 先 POST `${policyCenterDomain}/PromptCenterService/v1/policy/feature-toggle`，请求体包含 `department`。
- 从响应 `web_search_enable` 中按 `netWorkZone` 匹配 `area`，读取 `open_status`、`supplier`、`scene_code`。未配置网络区域或策略关闭时，客户端拒绝执行搜索。
- 搜索 POST `${knowledgeBaseUrl}/api/v1/websearch`。
- 请求字段：`scene_code`、`query`、`user_id`、`app_id: "CodeAgent3.0"`、`supplier`。
- 搜索函数明确构造 `x-auth-token`、`x-current-user`，公共请求辅助函数另合并头字段；完整公共鉴权头与网关现有会话的对应关系尚待核对。
- 响应读取 `data.refer_list`，每项使用 `document_url`、`chunk_title`、`chunk_content`，另有可选文档标识和文件名。
- 官方扩展将 `knowledgeBaseUrl` 设置为 `agentChatDomain`。安装包商业版默认域名为 `https://snap-access.cn-north-4.myhuaweicloud.com`，香港配置为 `https://snap-access.ap-southeast-1.myhuaweicloud.com`；实际值可能由服务发现替换，不能把默认值当作当前账号端点。

内核还包含 Exa 实现：POST `https://mcp.exa.ai/mcp`，JSON-RPC `tools/call` 调用 `web_search_exa`，参数包含 query、type、numResults、livecrawl、contextMaxCharacters。仅能确认打包了该实现，尚不能据此断言当前 CodeArts 会话实际启用它，或厂商搜索失败后会自动回退到它。

下一步应核对当前客户端实际启用工具、区域策略和已有会话鉴权，再用授权账号执行一次最小搜索。未执行真实 CodeArts 搜索请求，未证明此账号有搜索权限。

## Trae：官方工具已确认，独立接口尚未定位

官方文档 https://docs.trae.cn/ide_agent 的内置工具列表明确列出联网搜索，用于检索与用户任务有关的网页。官方更新日志 https://docs.trae.cn/ide_changelog 也记录 @Web 联网搜索。

本机常见安装位置未找到 Trae 客户端程序；未将“未找到”当作整个文件系统不存在的证明。检查已有网关适配说明及 connectedGraph/trae2api-web 的 main 分支 `docs/RESEARCH.md`、`internal/upstream/client.go`，未定位搜索协议。现有网关的 `/api/agent/v3/llm_utils_chat` 是模型通道，不能据此推断包含客户端搜索工具执行器。

下一步需要官方客户端实现或实际搜索网络请求，确定地区、鉴权、请求及返回结构，再做接口测试。尚未获得可验证的独立原生搜索地址。

## 与已有验证的区别

2026-11-19 的 `CODEARTS-TRAE-TOOLS-2026-11-19.md` 验证的是 function calling 两轮往返，不是厂商网页搜索服务，不能拿它证明原生联网搜索已接通。


## 网页端 webfetch 追踪：当前证据与阻塞

用户提供的 CodeArts 网页端截图显示：工具发现后使用 `webfetch` 访问 arXiv 搜索页，再访问论文详情页。截图未展示 HTTP 请求地址、响应或工具执行服务器，不能由此确认网页端独立抓取接口。

尝试连接浏览器时，计算机使用工具返回 `Codex auth token is unavailable`，无可见浏览器或标签页，因此尚未取得该网页会话的网络记录。

本机版本检查发现两个内核并存：26.10.0（旧）和 26.11.200（新）。旧内核的 `webfetch` 明确接受 url、format（text/markdown/html）、timeout，工具执行器直接 `fetch(url)`，然后转换内容；这条实现不需要调用华为专用网页抓取 API。默认超时 30 秒、最高 120 秒、响应上限 5 MiB。这只能说明旧桌面内核实现，不能用于断定当前网页后端使用相同代码。

新内核包含 webfetch 权限及展示引用，但本次未定位到相同的独立执行函数；不能将旧内核实现当作新版本的已验证行为。

继续追踪需要该网页会话的网络证据：在浏览器开发者工具 Network 保留记录后，执行一次已知网址的 webfetch，检查 Fetch/XHR、EventStream 或 WebSocket 中的工具事件。若只有会话请求和工具结果事件，则需要进一步判断是否由云端执行器抓取；浏览器中看不到直连 arXiv 请求，不等于没有抓取。分享记录前去除 Cookie、Authorization、Token 和无关会话正文。
