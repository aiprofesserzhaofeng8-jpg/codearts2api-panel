# P4b Command Code 分批验收

日期：2026-11-16。状态：源码接入与隔离验收完成，已注册到应用和面板；真实账号验收待完成。

## 已实现

- 固定 api.commandcode.ai 的 Provider API，API Key / global 范围；支持标记 subscription 或 payg，同一密钥不因标记变化变成第二个账号，不伪造 OAuth 或过期时间。
- 参考固定 Sub2API 的 Claude→Messages、GPT→Responses、其他对话型号→Chat 分流。厂商前缀原样保留，例如 deepseek/deepseek-v4-flash。目录必须声明对应 supported_endpoints，不把 System One 决策模型当成聊天模型。
- 共用已有三种无状态转换器。仅为 JSON 模型字段新增显式带厂商前缀的 Chat 编码入口，其他平台继续原来的型号校验；没有放宽端点 URL 或修改其他平台路由。
- 目录解析 context_length，及可选 max_output_tokens/modalities。未知字段不从型号名字猜测，也不采用官方 Pi 插件的推测性视觉/推理及 32K 输出默认值。
- Verify 先查受认证的 alpha/whoami，再读取目录并发一次短推理。免费模型的推理成功和公开目录返回均不能单独作为密钥有效证明。
- 额度读取 whoami 和 billing/credits，个人不携带 orgId，组织 ID 使用 URL 查询编码。月度剩余、充值、免费额度分别显示；5 小时和周窗口保留用量、总额、毫秒重置时间。未知不等于零。
- 额度只作展示，不新增限流、禁用、缓存、定时任务或数据库。充值积分能否绕过窗口以上游决定。
- 保留 LGPL-3.0-only 许可、固定提交和源文件/本地摘要。

## 验证

隔离 HTTP 测试覆盖凭据混用拒绝、三个协议的原始型号/图片/工具转发、公开目录筛选、身份失败终止、推理 403 拒绝导入、组织隔离、未知/零/非法额度、截断流和取消。

定向 `go test -race`（commandcode、opencode、cnapi）及相关包 vet 通过；日志 `/tmp/codearts-p4b-foundation.log`。架构检查及 diff 检查通过。

真实无凭据 GET https://api.commandcode.ai/provider/v1/models 在本机返回 HTTP 403，未获取实时模型快照，原因未确认。没有凭据推理测试。上述测试数据是按文档和 Sub2API 测试字段构造，不能声称真实平台通过。

官方当前说明 Go 套餐不开放外部 API，符合条件的订阅和 Provider 按量账户使用 API Key。不会通过伪装 CLI 绕过套餐边界。

## 本批补齐

- 注册第 18 个平台；面板提供订阅与 Provider 按量两种标记，隐藏浏览器授权，提示套餐限制与短推理费用。保存失败保留输入，成功清除密钥，中英文均有对应提示。
- 月度窗口查询 subscriptions 和 usage/summary，带当前周期 since，保持组织 ID。以 totalMonthlyCredits + 月度剩余还原总额，不使用可能包含充值消耗的 totalCost。周期末作为重置时间。
- 月度详情非认证错误时返回已有余额并显示部分失败提示；认证或取消错误直接终止。未知用量、负数或非法周期不产生虚假总额。
- 明确厂商/模型族可加入共享名称，完整型号仍原样转发；未知厂商、厂商/模型不匹配及版本后缀不误合并。
- 12 组网关场景验证原始/共享名称、三个客户端协议、流式/非流式、实际来源/用量/缓存计量、并发归零及分组权限；另有严格保存失败和重试、共享名称、月度异常用例。
- Linux/Windows/Docker 分发配置携带 LGPL 许可与来源清单。
- 定向竞态测试通过：`/tmp/codearts-p4b-integration-race.log`。前端类型检查、67 个单元测试及构建通过。

全量 `make check` 通过（`/tmp/codearts-p4b-check.log`）；新增前端部分失败提示后，类型检查、67 个测试及构建重新通过，生产资源/CSP/API 类型检查通过。浏览器 Command Code、OpenCode、官方 API、额度页面四项通过（`/tmp/codearts-p4b-browser/browser-results.json`）。Linux 与 Windows 构建通过，产物为 `/tmp/codearts-p4b-panel`、`/tmp/codearts-p4b-panel.exe`；未替换运行服务。

## 剩余真实验收

1. 获取获准调用的真实 API Key，分别验证订阅及 Provider 账号导入、推理、图片、工具往返和真实套餐窗口。
2. 查清本机无凭据目录 403 的原因；当前证据不足以判断是边缘网络、风控还是访问条件。
3. 验证真实模型目录字段、版本合并及客户端缓存表现。隔离测试不能代替真实平台验收。

未部署、未推送、未发布。下一阶段为 P4c Cline。

来源：[官方 Provider API](https://commandcode.ai/docs/provider)、[官方 Pi 插件](https://github.com/CommandCodeAI/pi-commandcode-provider)、[来源清单](../../backend/internal/commandcode/UPSTREAM.md)。
