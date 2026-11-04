# Kiro 协议来源与维护

独立 Go 适配器按项目现有 provider 接口编写，没有复制其他项目的账号池、持久化、重试、代理执行或计费模块。它调用 Kiro 原生端点，不调用 CLI/ACP，不执行工具，也不扫描用户主目录。

协议事实主要参考 MIT 项目 [xwteam/kiro2api](https://github.com/xwteam/kiro2api/tree/4e82d5819e395f2a9f39b62c04fc01f62a83e75d)，固定提交 `4e82d5819e395f2a9f39b62c04fc01f62a83e75d`：

- `src/kiro/endpoint.rs`、`credential.rs`、`refresh.rs`：地区、OAuth 刷新和 API Key 认证。
- `src/kiro/login/builderid.rs`：Builder ID OIDC 设备授权。
- `src/models_cache/client.rs`、`model.rs`：动态模型目录和上下文元数据。
- `src/kiro/wire.rs`、`convert.rs`、`eventstream/`：会话、工具和二进制事件流的字段形状。

API Key 的 Q 服务根路径与两个不同操作 target 另核对 MIT 项目 [satiyap/pi-kiro-api](https://github.com/satiyap/pi-kiro-api/tree/86c8f2da604fb6b0ee141b52ccc1fc540d4bb430) 的 `src/kiro/discover.ts` 与 `stream.ts`，固定提交 `86c8f2da604fb6b0ee141b52ccc1fc540d4bb430`。两类凭据不混用元数据端点。

参考项目的 MIT 许可保留在 [LICENSE.kiro](../../../LICENSE.kiro)，随 Linux / Windows 附件和镜像分发。此处的协议转换代码由本项目实现；没有采用参考项目的指纹伪装、模型猜测、字符估算计费或跨端点推理重试。

标准认证契约另核对 AWS 官方的 [RegisterClient](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/API_RegisterClient.html)、[StartDeviceAuthorization](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/API_StartDeviceAuthorization.html) 与 [CreateToken](https://docs.aws.amazon.com/singlesignon/latest/OIDCAPIReference/API_CreateToken.html)。登录选项见 [Kiro 认证说明](https://kiro.dev/docs/getting-started/authentication/) 与 [CLI 命令说明](https://kiro.dev/docs/cli/reference/cli-commands/)。

## 实现约束

- 数据面固定在账号地区的 `q.<region>.amazonaws.com`。OAuth 使用 `/generateAssistantResponse`；API Key 使用根路径和 `AmazonCodeWhispererStreamingService.GenerateAssistantResponse`，附带 `TokenType: API_KEY`。不在失败后探测其他数据端点或重复生成。
- 目录使用 `AmazonCodeWhispererService.ListAvailableModels`；API Key 使用同地区 Q 根路径，OAuth 美国使用 CodeWhisperer 元数据域名、欧洲使用同地区 Q 域名。目录分页有界；不添加静态猜测型号、窗口或价格。
- Kiro CLI 的 `runtime/management.<region>.kiro.dev` 是另一协议入口；本实现选择上述 AI_EDITOR 协议，没有把不同入口和认证方式当作可以任意替换的镜像。
- `region` 是数据地区，`auth_region` 是 OIDC / social 发令牌地区；显式导入时保持两者独立。profile ARN 的地区必须与显式数据地区一致。
- OIDC 设备授权按上游轮询间隔执行，`slow_down` 加长间隔；10 分钟内存会话、128 项上限、取消即失效。链接严格限制官方 HTTPS 域名。
- 刷新响应处理后立即返回凭据，由已有账号协调器完成条件提交，不在轮转和提交之间追加目录/资料请求。
- CRC 校验完整 AWS 帧，损坏即失败，不尝试跳过坏字节后宣布成功。单帧 8 MiB，工具参数累计 8 MiB；工具 JSON 和大整数保留原始表示。正常帧边界的 HTTP EOF 是该协议的结束方式，无法识别上游在完整帧之后错误地正常关闭连接的情况。
- 仅转发上游明确返回的 token 计量；credits 和上下文占用百分比不冒充 token 或美元费用。未知计量继续沿用网关原有未知用量规则。

## 更新与验收

协议可能随 Kiro 客户端更新。变更前核对实际端点与脱敏捕获，再更新本目录的模拟上游测试；不得把真实凭据或请求内容加入夹具。真实账号登录、模型生成、API Key 路由和到期续期尚未验收，不能据模拟测试宣称上游长期可用。
