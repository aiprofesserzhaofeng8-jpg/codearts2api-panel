# AWS Bedrock / Claude

在账号管理选择 AWS Bedrock，导入 JSON：

```json
{
  "aws_access_key_id": "YOUR_ACCESS_KEY_ID",
  "aws_secret_access_key": "YOUR_SECRET_ACCESS_KEY",
  "aws_region": "us-east-1",
  "model_ids": ["us.anthropic.claude-sonnet-4-5-20250929-v1:0"]
}
```

示例型号不是账号授权保证。model_ids 可选：填写时仅使用指定型号，避免要求目录权限；省略时调用 ListFoundationModels 与 ListInferenceProfiles，选取支持流式的 Claude 基础型号及 ACTIVE 系统推理配置。导入校验会发送一次最多 64 输出 token 的短请求，可能收费；不会自动尝试另一型号或区域。

临时凭据还需 aws_session_token 和 Unix 秒 expires_at；可使用到实际到期时间，到期后需重新导入。不会自动续期或读取本机 AWS 配置。提前五分钟刷新仅适用于有续期材料的账号，长期密钥不填写这两个字段。

现有 Chat Completions、Responses、Messages 入口复用此账号，来源为 bedrock/<aws_region>/<AWS型号>。可在统一模型管理中显式配置共享名；不擅自将带日期/跨区域配置的型号合并或替换。只转换至 Bedrock InvokeModelWithResponseStream，不宣称 Converse 或原生 Responses 支持。

当前范围：商业区域的 Claude Messages 模型与系统推理配置 ID。暂不支持中国/GovCloud 分区、ARN、自建应用推理配置、AssumeRole、自动 STS 续期或 Bedrock bearer API Key。AWS 余额显示未接入；实际账单与模型权限由 AWS 决定。

工程隔离验收已执行，真实 AWS 验证尚未完成。详见 acceptance/SUB2API-PLATFORM-P5A-2026-11-16.md。
