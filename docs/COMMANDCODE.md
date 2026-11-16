# Command Code 接入

账号管理 → 添加账号 → Command Code → 导入。选择有外部 API 权限的订阅或 Provider 按量账号，填写自己的 API Key。不是浏览器 OAuth；Go 套餐当前不开放外部 API。

导入先校验账号，再发一次短推理请求，可能消耗额度。公开目录或免费模型成功不能独立证明密钥有效。相同密钥切换本地套餐标记不会创建第二个身份，真实权限以上游为准。

型号来源为 `commandcode/global/<原始型号>`，如 `commandcode/global/deepseek/deepseek-v4-flash`。请求保持完整厂商前缀；共享名称仅对已确认的厂商与模型族移除厂商前缀，保留全部版本后缀。未知厂商、别名或不匹配的型号不会任意合并。

Claude 使用 Messages，GPT 使用 Responses，其余已声明支持的对话型号使用 Chat Completions；不把 System One 决策接口当作聊天接口。模型目录必须声明对应 supported_endpoints。参数读取已返回的 context_length、max_output_tokens 和 modalities；缺失值保持未知。

额度查询沿用既有缓存及刷新入口，没有新增后台任务、限流或数据库。个人查询不携带 orgId；组织查询只使用 whoami 返回的组织 ID。

月度总额仅由本周期 `totalMonthlyCredits` 加月度剩余计算，重置时间使用订阅周期末。充值/免费积分单独显示。月度接口暂时失败时显示提示、保留已知余额，认证失败则整个查询报错。没有把 totalCost 混入月度套餐消耗，也不根据窗口满额在网关自行禁用账号。

源码和隔离验收不能证明真实套餐可用。目前未使用真实 Command Code 密钥测试；本机无凭据读取公开目录返回 403，原因未确认。运行服务尚未升级。

来源：[官方接口](https://commandcode.ai/docs/provider)、[来源及许可](../backend/internal/commandcode/UPSTREAM.md)。
