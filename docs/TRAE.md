# Trae CN / SOLO 接入（开发版）

版本 `0.3.0-dev`，位于 `develop`。码道基线 `v0.1.0` 和 WorkBuddy 基线 `v0.2.0-preview.1` 保留。

## 能力范围

- 原生国内 CN / SOLO 通道：`/api/agent/v3/llm_utils_chat`，使用 `solo_work_lite`，不是指向其他中转的通用代理。
- 统一接入 Chat Completions、Responses、Messages，支持文本、思考字段、结构化工具调用及工具结果历史转换。
- 上游 `output` / `token_usage` / `done` 自定义 SSE 转成标准 Chat 分片；没有明确 done、格式错误、半截工具参数均返回失败，不伪造完成。
- 模型清单来自账号可访问的 `get_detail_param`，不虚构可用模型或上下文上限。
- 原生凭据续期和身份验证；个人积分数据有实际字段才展示，未知或不可计量额度不会当成零余额。
- 暂未接入国际版、Remote / Work 云端执行通道、图片和音频、自动签到、Trae 原生上下文压缩。

## 添加账号

在账号池点击「添加 Trae」。当前只有国内地区可选。

### 浏览器授权

1. 点击生成链接，复制到自己信任的本机浏览器，或点击打开。
2. 完成 Trae 官方授权后，会跳转到 `http://127.0.0.1:18080/authorize?...`。
3. 复制地址栏的完整 URL，返回面板粘贴并点击检查。该 URL 含登录凭据，输入框会隐藏内容，提交成功后清空。
4. 核对账号预览，再确认保存。已有账号须明确确认更新。

当前使用手动粘贴回调，不在浏览器机器上自动启动监听器。没有本地监听器时，回调页可能显示无法连接；请确认使用可信本机环境且 18080 端口未被其他程序占用，然后复制完整地址。若浏览器未保留完整回调或官方流程不接受此方式，可用桌面端的完整凭据导入。远程服务器的 localhost 与浏览器机器的 localhost 不同。

回调必须与本次 `login_trace_id` 匹配，且只接受预设的 loopback 路径；服务端不会访问粘贴 URL，也不会信任其中的任意目标主机。回调中的身份会向官方 GetUserInfo 核对。检查仅保留预览，确认才入库；重复确认不会新增账号，删除后重放旧确认不会恢复账号。

### 凭据导入

支持扁平 JSON 和 `auth` / `account` 嵌套 JSON。示例为占位符，不包含真实凭据：

```json
{
  "access_token": "YOUR_CLOUD_IDE_JWT",
  "refresh_token": "YOUR_REFRESH_TOKEN",
  "expires_at": 1900000000,
  "uid": "YOUR_USER_ID",
  "scope": "cn",
  "client_id": "en1oxy7wnw8j9n",
  "machine_id": "YOUR_EXISTING_MACHINE_ID",
  "device_id": "YOUR_EXISTING_DEVICE_ID"
}
```

兼容 accessToken / refreshToken / expiresAt / machineId / deviceId；过期时间支持秒、毫秒和 RFC3339，JWT exp 可作缺失时的时间提示。JWT 解析不代替认证，导入仍需官方身份接口验证。SOLO client_id 为 `en1oxy7wnw8j9n`；已有 IDE 凭据如明确使用 `ono9krqynydwx5`，需保留实际 client_id。是否有当前原生通道权益仍以上游为准。

不要同时让多个服务争用同一份会轮转的 refresh token。进口凭据中的 apiHost、host 等字段不能改变固定的官方请求目的地。

## 模型和调用

保存账号后刷新模型目录，使用实际返回的模型配置 ID：

```text
trae/cn/<config_name>
```

例如目录返回 `glm-5.2` 时，客户端填 `trae/cn/glm-5.2`。模型前缀只在网关内用于分流，原生请求发送不带前缀的 config_name。裸模型名仍保持码道语义，不会自动换成 Trae。

原生工具定义的 parameters 需要 JSON 字符串，适配器负责转换；工具历史的 function 转为 function_call。工具仍由 API 调用方执行。对不支持或无法忠实转换的输入明确拒绝，不静默删除图片、工具或改变指定模型。

预算和计价使用完整模型 ID，不能继承码道或 WorkBuddy 同名模型价格。没有上游 usage 时保持未知；USD 预算需要自行配置该模型参考价格。Trae 暂无自动验证过的上下文推荐时，客户端启动器会提示配置模型策略。

## 验证状态

本轮自动化验证包括：三种 API 的流式/非流式转换、思考与缓存用量、工具分片/多工具、截断失败、凭据目的地与重定向边界、刷新令牌保留、回调 trace 绑定、显式确认保存、删除后重放、上游身份核对、未知额度以及账号/用量隔离。

本机没有已登录的 Trae 账号，尚未完成真实授权、模型生成、额度及过期续期验收。安装包存在不代表已有可用登录态。开发版提供可测试入口，不将模拟测试标为真实上游验收通过。

来源与许可证见 [原生适配来源](../internal/trae/UPSTREAM.md)。
