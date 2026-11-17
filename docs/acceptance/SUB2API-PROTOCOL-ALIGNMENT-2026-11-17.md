# 协议对齐验收（2026-11-17）

源码完成，未提交或推送；已按用户授权升级本地受管服务。参考 Sub2API
`ba81c7fd758e425d67f856d601147ef0c5263d3f`。

## 已实现

- Messages / Responses 入口保留原始请求，能力筛选不再依赖完整 Chat 转换。
  账号选择、权限、预算、租约仍使用现有原子准入。
- Claude Messages、ChatGPT/Codex Responses 及明确选中相同协议的多协议
  平台直接保留原生 JSON 事件。非流式客户端收集原生结果；跨协议继续调用
  既有转换器。旧 Chat 入口及专用平台适配器保持可用。
- Kimi、DeepSeek、MiniMax、Zhipu 使用现有账号模式的官方端点。
  缺少 api_protocol 保留 Chat；新建账号界面显式保存 adaptive。
  Zhipu 没有 Responses 选项，自适应 Responses 请求走 Chat 转换。
- OpenCode / Command Code 支持固定协议、自适应及导入凭据中的
  protocol_rules。显式规则先于目录，目录先于内置规则；配置规则未命中且
  无目录时回落 Chat。旧账号缺协议字段保持原来的按型号端点。
- 新增账号的协议选项、账号操作菜单中的协议编辑，均使用原管理接口。
  配置保存使用凭据代际 CAS 与失败回滚，不覆盖并发更新的凭据。
- 原生用量直接进入现有账本；统一模型名称只改响应包络的 model。
  提示词策略在原始协议上执行；已输出后禁止重放，截断流不伪造成功。

## 验证证据

- `make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` 通过：
  前端类型、单元测试、构建、OpenAPI 同步、Go 全仓测试/vet、架构边界、
  控制台资源检查、兼容检查及部署工具测试。
- 后续单次执行接入及 Codex 包络兼容调整，运行对应 providerhttp、server、
  upstream 定向测试；结果通过。
- storage/server/gateway 定向 race 通过；最后的原生转发、账号协议编辑、
  映射和输出后不重放测试再次通过 race。
- `scripts/protocol-settings-smoke.cjs`：中英文、新建默认值、编辑回读、
  保存失败保留选择与重试、360px 页面无横向溢出通过。截图已人工检查：
  `artifacts/protocol-settings-zh-CN.png`、
  `artifacts/protocol-settings-zh-CN-mobile.png`（构建产物不入库）。
- 原有三入口 × API Key/OAuth × 流式/非流式 × 文本/工具回归通过。
  原生专用测试检查未知字段、大整数、原始 ID、签名、缓存用量、租约释放。
- 模拟 Responses 上游补充真实协议要求的 response.created 和终态 output；
  不再使用只适合 Chat 转换器拼装的残缺 Responses 测试响应。

## 使用和兼容边界

客户端仍通过 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`
及既有别名调用同一公开模型名。账号协议决定上游端点，不改变来源调度权重。
已有账号可在账号列表“更多 → API 协议”选择自适应，不必重新输入密钥。

平台端点保留本项目既有地区/域名；MiniMax 保留 api.minimax.cn。
没有导入 Sub2API 的自定义 URL、第二个目录缓存或网络探针。
Codex 延续原适配器行为：store=false，字符串 input 转为消息数组，
不向订阅后端发送 max_output_tokens（该参数仍不是订阅端输出上限保证）；
API Key Responses 保留该参数。Messages 缺省 max_tokens 仍为 4096。

previous_response_id、store=true、后台任务、WebSocket 等限制保持。
本次为隔离假上游与本地页面验收，未调用真实平台推理、刷新共享凭据或验证套餐权限。

## 本地部署

已部署 `v0.4.17-local.202611171722` 至 `/opt/codearts2api`，服务
`codearts2api.service` 运行正常。`http://127.0.0.1:10087/health` 返回
8 个账号、8 个就绪、123 个模型；线上 JS/CSS 与当前构建一致。
受管升级保留旧版本与升级前数据备份；部署记录位于
`/home/white/backups/codearts2api/protocol-alignment-20261117/DEPLOYED.txt`。
