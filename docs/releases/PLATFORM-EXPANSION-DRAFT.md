# 平台扩展发布说明草稿

状态：未发布。版本号待正式发布时确定；本地 v0.4.14-local.* 是验收包，不是新稳定版。

## 本次变化

- 新增 DeepSeek、智谱、Kimi、MiniMax 官方 API 接入及已实现的国内个人 Coding 套餐模式。
- 新增 Gemini API / CLI、Antigravity、Grok Build、OpenCode Zen/Go、Command Code、Cline。
- 新增 TypeSafe 原生决策接口，不把它当作聊天模型。
- 新增 AWS Bedrock 商业区域 Claude 和 Vertex AI 服务账号 Gemini 接入。
- 沿用现有账号导入、共享模型、分组权限和用量账本；保存失败不显示成功。
- 不可续期临时凭据在实际过期前仍可使用，不再被五分钟刷新窗口提前拒绝。
- 协议转换保留可识别的工具元数据和推理扩展；没有新增文件存储、签名缓存、数据库或全局重试层。

## 验证状态

工程隔离测试已通过，包含三种客户端入口的适用路径、共享名称、权限、保存失败和凭据轮转。Linux amd64 安装、恢复、独立 systemd 升级及 Docker 临时环境验收通过。Windows amd64 和 Linux arm64 包完成构建/摘要验证，但本机未完成它们的原生运行。

新增原生 ARM CI 与发布门禁，远端工作流尚未运行。正式发布必须使用提交后的干净源码，等待 Windows/ARM 验收并使用被验收的制品。不能直接把旧的 local 验收包改名发布。

## 必须告知用户的限制

- Gemini CLI/Antigravity OAuth 不再内置第三方客户端 ID/Secret。管理员需要配置对应 OAuth 环境变量后重启；否则新登录和后续续期不可用。Gemini API Key 模式不受影响，详细配置见对应平台文档。

- 各新增平台的真实账号/订阅/IAM、图片识别、多轮工具和自然续期仍需实际授权账号逐项验收；模拟通过不等于真实可用。
- Pi 1.4.0 Chat 路径会丢失 Gemini 工具签名；Pi Messages 会丢失 Cline reasoning_details。Cline Chat 两轮及 Responses 转换可保留扩展，但未证明真实上游签名有效。
- Vertex 目录当前为显式 model_ids，未实现自动发现；不含 Vertex Claude、Express API Key 或主机 ADC。
- Bedrock 不含中国/GovCloud、ARN、自建应用 profile、AssumeRole、自动 STS 续期和 bearer API Key。
- Gemini CLI 参考目录不等于实时账号目录；MiniMax 当前 M Plan 额度响应仍需真实确认。
- 没有远程文件下载/托管文件上传或媒体生成兜底；不支持的输入不能静默丢弃或换型号。
- TypeSafe 独立目录/页面按用户要求跳过。

## 发布与升级前

1. 完成源码审阅和提交，运行 GitHub CI；选择正式版本号后重新生成制品。
2. 核对包内第三方许可、来源清单、制品摘要及对应提交。
3. 发布记录逐项区分工程通过、真实通过、发布及部署，不写“所有平台已真实验证”。
4. 运行服务升级前备份，按现有安装工具流程升级；本次验收没有改动用户生产数据。

完整证据与未完成项见 [P6 验收](../acceptance/SUB2API-PLATFORM-P6-2026-11-16.md)。
