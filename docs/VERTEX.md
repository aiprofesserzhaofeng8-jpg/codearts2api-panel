# Vertex AI / Gemini

在账号管理选择 Vertex AI，导入以下结构：

```json
{
  "service_account": {
    "type": "service_account",
    "project_id": "your-project-id",
    "private_key_id": "YOUR_KEY_ID",
    "private_key": "-----BEGIN PRIVATE KEY-----\nYOUR_PRIVATE_KEY\n-----END PRIVATE KEY-----\n",
    "client_email": "your-service@your-project-id.iam.gserviceaccount.com"
  },
  "location": "global",
  "model_ids": ["gemini-2.5-flash"]
}
```

service_account 可直接放入 Google 下载的服务账号 JSON 对象。项目默认使用其中的 project_id；跨项目调用可在外层指定 project_id，权限由 Google IAM 决定。location 默认 us-central1，示例使用 global；模型和区域是否可用以该账号实际授权为准。

导入会使用私钥换取新访问令牌，再对 model_ids 的第一个型号发送最多 64 输出 token 的短请求，可能收费。认证/推理/保存任一步失败均不会显示导入成功。首次不需要手填 access_token；后续复用现有自动续期设置和凭据保存流程。

本批目录来源为明确配置的 model_ids（最多128个）；不是上游自动发现，也不证明每个型号可调用。型号按原样发送，不自动替换区域、项目或模型。不猜测上下文窗口和多模态能力；实际传输复用已有 Google 原生协议转换。

支持现有 Chat Completions、Responses、Messages 入口。来源名称为 vertex/<location>/<model>，可绑定现有统一模型与分组。余额尚未接入，不能把未知余额当作零。仅支持服务账号与 Gemini 内容生成；不支持 Vertex Claude、API Key Express、主机 ADC、托管文件上传或生成媒体接口。

工程隔离测试通过不等于真实 Google Cloud 验收。见 [P5b 验收](acceptance/SUB2API-PLATFORM-P5B-2026-11-16.md)。
