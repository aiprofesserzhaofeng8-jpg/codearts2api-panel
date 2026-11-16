# P5a Bedrock：签名与协议基础

日期：2026-11-16。状态：底层签名、请求编码、事件流转换与隔离测试完成；尚未注册为可用平台。不是完整 Bedrock 接入。

## 本批范围

复用固定 Sub2API 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484：

- bedrock_signer.go：AWS 官方 SigV4 包装，删除 Account 依赖。aws-sdk-go-v2 v1.41.5 和 smithy-go v1.24.2 固定版本，与上游一致。ADR 0001 记录此局部依赖例外。
- bedrock_stream.go：有界二进制帧、两级 CRC 校验，异常不回显上游载荷。每次读取一个帧，经现有 messagewire 转换；没有第二套队列、goroutine、全响应缓存或落盘。
- 原生请求规则：模型保留在 URL，转义冒号与路径字符，注入 bedrock-2023-05-31，移除 wire 层 model/stream。复用本项目工具/图片/消息转换，不搬入上游 CC 型号特判和自动区域改写。
- 用量：只在缺标准 usage 时转换 invocationMetrics，缺字段不造零，标准 usage 优先，大整数及工具参数不经 float 解码。
- 当前 URL 构造仅覆盖商业区域；中国/GovCloud、完整 ARN 校验属于下一批，未对用户开放任何入口。

## 验证

模拟帧测试覆盖文本/工具流、工具参数大整数、图片编码、CRC 损坏、超长和截断帧、缺结束事件、坏 base64/JSON、取消、未知用量、异常信息脱敏；签名验证请求体/区域/服务/临时令牌参与计算。

定向竞态日志：/tmp/codearts-p5a-wire-race.log。没有真实 AWS 请求，没有身份或 IAM 权限验证。取消测试为已取消上下文；正在阻塞 HTTP 读取的取消需在实际传输接入后验证。

## 下一批

1. AWS 凭据规范化、商业区域及 ARN/profile 约束、静态/临时凭据生命周期；不自动读取主机云身份。
2. HTTP 签名传输、真实错误分类、模型目录/导入验证，确保目录成功不当作推理成功。
3. 应用注册、账号表单、权限/用量/共享模型集成；不提前给出可用入口。
4. 有真实 AWS 授权账号后验证文本、工具、图片、事件流与计费。随后 P5b Vertex AI。

源代码、Linux/Windows/Docker 分发增加 LGPL 来源和 AWS/Smithy Apache 许可证；没有部署、推送或发布。

最终检查：全量 make check 通过（/tmp/codearts-p5a-check.log）；四项打包测试通过（/tmp/codearts-p5a-package.log）；go mod verify 通过；Bedrock 测试包 Windows 交叉编译通过（/tmp/codearts-p5a-bedrock.test.exe），没有在 Windows 执行测试。

## 第二批：客户端、目录与应用接入

2026-11-16：平台已注册，账号管理可导入 AWS JSON，使用现有 ProviderLogins/ProviderCredentials/Admission/账本。第一批“尚未注册”的描述是历史状态。

- 长期密钥及带 expires_at 的临时令牌，身份按 access key + region 稳定区分。临时凭据不假称可自动续期，过期请求不发上游。
- 固定控制面/运行时主机，使用 SigV4，不加载本机 AWS 环境/配置文件，不接受自定义 URL。
- 目录读取流式 Claude 基础型号、ACTIVE 系统推理配置，分页32页上限与重复token检测；支持显式 model_ids，省去目录权限。保持型号原样，不尝试替换区域前缀。旧 Claude v2/Instant 排除。
- 导入验证一条短推理，目录可读不代表 IAM 推理权限可用。权限失败、保存失败不报成功，保存失败可重试。
- HTTP 凭据错误、权限、限流和流内异常分类，不透传上游私密错误内容。流内无法分类错误按502处理。
- 三种客户端协议、共享名、用量12/3、来源、分组拒绝与并发释放模拟验收；中英文 JSON 导入、失败重试和密钥清空浏览器回归通过。
- 共享名称支持手动绑定；本批未新增 AWS 日期型号与公共原生版本的自动别名表。

测试记录：/tmp/codearts-p5a-integration-race-final.log；/tmp/codearts-p5a-client-browser/browser-results.json。

仍需真实 AWS 账号验收；中国/GovCloud、ARN/application profiles、AssumeRole、自动 STS 续期、bearer API Key、完整 beta/缓存策略不在本批支持范围。账号未授权时不能标记真实通过。下一独立阶段为 P5b Vertex AI。

未读取生产状态，未部署、未推送、未发布。

已确认待处理：现有 ProviderCredentials 在到期前五分钟进入刷新分支，未按 HasRefresh 区分无续期材料的有效临时凭据。Bedrock 临时令牌在这段窗口会要求重新导入；长期 AK/SK 不受影响。本批未为单个平台增加刷新特例或伪造到期时间，后续须补齐通用凭据协调器的到期边界回归，再称临时凭据完整可用。

第二批最终检查：全量 make check 通过（/tmp/codearts-p5a-client-check-final.log），定向竞态和浏览器测试通过。Linux /tmp/codearts-p5a-client-panel、Windows /tmp/codearts-p5a-client-panel.exe 构建通过，未安装。早期默认平台数量测试仍期待19个，已随Bedrock加入改为20个聊天平台；最终检查包含该回归。

## 第三批：临时凭据到期边界

2026-11-16：上文“到期前五分钟提前拒绝”的已知问题已修复。共享 ProviderCredentials 根据 Normalize 返回的 HasRefresh 判定是否可续期：不可续期凭据在实际到期前直接使用，到期后返回 401 并要求重新登录或导入；可续期凭据保留五分钟窗口、实时续期开关、按账号互斥与 CAS 提交。长期密钥保持 NonExpiring 语义。没有新增缓存、锁、状态字段或后台任务。

隔离回归覆盖：无续期凭据窗口内有效/过期、全局续期开关开关两种情况、可续期凭据窗口内/窗口外/过期及禁用续期。既有取消、并发只刷新一次、身份改变拒绝保存、保存失败保留轮转凭据测试保留。Bedrock 真适配器配合假 HTTP 传输验证临时令牌在最后一分钟可完成签名短推理、到期拒绝、无状态写入。不是 AWS 实时验证。

验证通过：/tmp/codearts-p5a-expiry-race.log（accounts、bedrock、gemini、grok、antigravity 竞态测试）；/tmp/codearts-p5a-expiry-check.log（全量 make check）。未读取生产凭据、未替换运行服务、未推送或发布。下一阶段为 P5b Vertex AI，真实云账号验收仍待完成。
