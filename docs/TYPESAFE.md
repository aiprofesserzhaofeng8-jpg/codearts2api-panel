# TypeSafe / System One

在账号管理添加 TypeSafe，导入自己的 API Key。验证读取上游受认证模型目录，不发送付费决策。此平台不提供聊天流，不会向 Pi/Codex 的普通模型目录加入 Jev。

调用面板的 `POST /v1/systemone`，使用面板 API Key，而非把 TypeSafe 上游密钥交给客户端：

```json
{"model":"jev-latest","state":"用户要求退还重复扣费","questions":{"billing":{"type":"noul","instructions":"是否是账单问题？"}}}
```

型号使用 TypeSafe 原始名称。权限和账本中的来源 ID 是 `typesafe/global/jev-latest`。同一接口支持文本/结构化状态的 Noul、Choice、Score；不支持 stream=true、图片 assets、Box 或 multipart。不会把问题改写成聊天消息。

请求沿用现有 API Key、用户预算、分组/型号权限、账号凭据和并发租约。一次请求只尝试一次上游决策，不自动换号重复调用。上游用量缺失时按未知记录；不把概率或状态长度当作 token。

当前已完成原生接口与面板的隔离测试，尚未真实账号验证。独立决策模型目录的面板展示、模型白名单配置便利性及完整用户预算验收仍待补齐。暂未支持共享聊天名称或余额查询。
