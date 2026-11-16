# Cline 接入

账号管理 → 添加账号 → Cline → 导入，填写自己在 Cline 控制台创建的 API Key。首批不提供浏览器 OAuth 或客户端令牌续期。

导入通过受认证的 users/me 验证，不发送付费推理。认证成功不代表某个型号有调用权限。公开模型目录同样不能作为密钥验证。

同一个密钥可以使用按量积分、`cline-pass/*` 订阅型号和 `cline-free/*` 免费型号。原始型号保持完整，统一走官方 Chat Completions 端点。当前默认名称保留来源区分，不自动把 Pass/免费前缀当作厂商前缀去掉。管理员可按已核实的型号身份配置共享名称。

积分余额与 ClinePass 五小时/七天/三十天窗口分开显示。微积分按精确十进制转换为美元；缺失值保持未知。无订阅的 404 不影响积分余额，认证失败不会被当成无订阅。

额度错误只影响实际失败的型号，复用既有短冷却和 Retry-After；不禁用整个账号、不创建钱包级冷却、不自动换到付费型号。与 Sub2API 一次冷却同钱包全部型号的方式不同，其他型号仍由上游确认权限。

普通 reasoning 文本映射到网关规范字段；reasoning_details 在 Chat 流和非流式聚合中保留，Chat 两轮工具往返已通过隔离测试。Responses/Messages 通过 reasoning_details 扩展字段透明往返，网关两轮测试已通过；不把它伪装成原生签名。本机 Pi Responses 离线转换已验证能回传该扩展，其他客户端若丢弃未知字段则不支持。本机 Pi 1.4.0 Messages 两轮实测会丢弃该扩展，不支持这类 Cline 推理历史往返；同版本 Chat Completions 两轮与 Responses 转换可保留扩展。真实上游签名验证仍待完成。

目前完成源码及隔离接入，没有真实 API Key 验收；运行服务未升级。

[验收记录](acceptance/SUB2API-PLATFORM-P4C-2026-11-16.md) · [来源与许可](../backend/internal/cline/UPSTREAM.md)
