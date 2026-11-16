# P2b：Gemini CLI / Code Assist

2026-11-15。本地实现及隔离验收完成，**真实 Google 账号与客户端验收未完成**。P2不能据此标为端到端全部完成。未修改生产状态/运行程序，未提交、推送或发布。

## 实现

- `gemini` 同时提供 api / cli 两个 scope；身份摘要区分认证模式及 CLI Google subject/project。不同通道不混用密钥/端点，也不新增中立账号字段或状态 schema。
- 浏览器授权复用现有有界 PKCE，新增可选 AccountScope（旧平台默认 global 不变）。固定 Google 端点；state、回调匹配和 PKCE 校验不放宽；无新 SessionStore、清理goroutine或数据库。
- 授权码兑换先由 PKCE 缓存，随后查询 Google userinfo 与 loadCodeAssist。后续验证或保存失败可以重试，不重复兑换一次性授权码；预览不保存，确认保存沿用 ProviderLogins。
- 新项目使用上游明确提供的默认套餐；onboard只发一次写请求，随后GET操作结果（最多5次，响应终态检查）。上游拒绝不绕过。没有项目时不会遍历其他云项目或代选付费套餐。
- 刷新只请求token端点，保留或更新refresh_token；新代际立即交回既有ProviderCredentials/Store CAS，不在轮转后增加可选网络调用。不会把旧令牌写回。
- CLI生成使用model/project/request包装及原生SSE；复用固定Sub2API schema纯函数；图片、工具参数/结果、签名、缓存与思考用量转换。明确终止帧才成功；取消和emit错误传回；不重复请求、不静默改型号。
- 只读配额使用Google官方CLI公开源码中retrieveUserQuota字段，复用QuotaWindow。缺失与真实0区分，剩余比例不变成总额/美元余额，不增加调度限制。API Key余额保持原有未提供。
- CLI目录使用Sub2API固定参考清单并标明出处，不冒充实时授权模型列表，不填造上下文或能力。模型更新后同名api/cli可分别作为共享来源，分组约束不扩大。
- UI支持Gemini浏览器授权、API Key导入、CLI JSON导入，预览显示cli范围；成功清理凭据，保存失败可重试。

## 直接复用与差异

Sub2API固定SHA：`530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484`。

直接复制并改包名/加出处：OAuth常量、TokenResponse、CodeAssist项目类型、参考模型列表；抽取`cleanToolSchema`及其纯辅助函数。HTTP/登录/存储依赖改为本项目现有窄接口，没有复制Gin/req SDK/数据库/调度或额外后台清理。

项目、生成包装与用量字段参考其OAuth/网关服务。没有沿用伪thoughtSignature，也没有把上游返回错误转换成成功。

另以Google官方Gemini CLI固定SHA `4ed06ed0ddc523a6d356a4ad35ddfea331041888` 核对操作GET URL、`GEMINI_CLI` metadata和quota字段；本地metadata不再使用Sub2API中的ANTIGRAVITY值。原始/本地摘要及许可见 [UPSTREAM](../../backend/internal/gemini/UPSTREAM.md)、[清单](../../backend/internal/gemini/upstream.json)。源码、Linux/Windows包和Docker许可COPY已补齐。

## 验证

| 项目 | 结果 |
|---|---|
| 认证模式 | api/cli隔离、缺字段/混合密钥拒绝、固定端点、静态与过期身份分别处理 |
| OAuth | state错误不兑换，PKCE参数正确；userinfo失败后重试只兑换1次；取消会话不能复活 |
| 保存 | 真实Adapter + ProviderLogins，预览无写入、模拟磁盘失败不假成功、重试保存成功 |
| 项目 | 新账号默认套餐初始化、已注册跳过onboard、身份/项目冲突拒绝、load拒绝不写、缺默认套餐不代选；操作仅GET轮询、取消及时结束 |
| 续期 | 8个并发Resolve只刷新一次，刷新令牌轮转持久化，重开Store验证；刷新后无额外身份/目录请求 |
| 原生协议 | system/图片/工具声明/工具结果/签名/9007199254740993保留；空参数工具变成{}；无效输入不静默丢弃 |
| 流与错误 | 空流/断流/非法结束/业务错误失败；401/403/429/500/重定向不成功，响应敏感正文不外泄；取消/emit失败传播 |
| 文本网关 | CLI原始/共享 × Chat/Responses/Messages × 流/非流12组合，来源cli、计量准确、在途归零；撤权403且不重复上游请求 |
| 签名工具网关 | CLI共享名称，Messages/Responses × 流/非流4组合，两轮原生函数工具签名和结果保留；Chat原生Adapter两轮另有测试 |
| 自动归并 | api与cli同名模型生成独立来源，未互相覆盖 |
| 配额 | 缺字段未知、0剩余保留、比例换算、时间解析；非法数字/NaN/负数/非法比例/时间失败，不捏造总额 |
| 浏览器 | official-api-smoke、gemini-cli-smoke、account-create-smoke通过；中英、浏览器/导入切换、scope、预览与保存失败重试 |
| 回归 | 全量make check及针对性race；Linux/Windows交叉构建；许可归档测试。详细状态以本轮检查日志为准 |

临时证据：`/tmp/codearts-p2b-adapter-final.log`、`/tmp/codearts-p2b-gateway.log`、`/tmp/codearts-p2b-final-check.log`、`/tmp/codearts-p2b-browser/browser-results.json`。全部使用合成凭据、临时HTTP服务和临时状态；未读取生产Google凭据或调用真实付费推理。

## 尚未通过的验收

1. 真实Google账号：授权回调、初始化/项目权限、真实生成、图片识别、配额及自然到期续期。当前没有真实账号结果，不能把httptest响应当上游可用性。
2. Pi签名回传：Pi 1.4.0 OpenAI-compatible通路丢弃extra_content。网关与CLI原生转换保留签名，并不能替客户端恢复已经丢失的信息；没有为此引入存储或伪签名。
3. CLI实时模型发现：当前是固定参考目录，不是账号专属实时可用型号；更新参考清单与真实权限应分开验收。
4. 不在本批支持范围：任意远程文件/图片下载、音视频、媒体生成、其他OAuth客户端或云项目自动选择、Google Cloud/Vertex服务账号。
5. 运行与GitHub交付未执行；当前新增功能仍在本地源码和临时构建中。
