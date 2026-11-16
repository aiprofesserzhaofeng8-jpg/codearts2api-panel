# P0：Sub2API 平台扩展复用准备

日期：2026-11-15。本文件保留 P0 的审查快照。P1 后续已完成工程及隔离验收，见 [P1 验收](../../acceptance/SUB2API-PLATFORM-P1-2026-11-15.md)；实际复制记录在 backend/internal/cnapi/UPSTREAM.md，此处清单中的 migrated=false 表示 P0 时点。

上游：https://github.com/Wei-Shaw/sub2api

固定提交：`530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484`。
本地基线：`d191c5deaeef8af189fffb5bfdfda242aea35db2`（v0.4.14 后的文档提交）。

[机器可读清单](upstream.json) 包含 22 个已下载核对的文件、字节数、SHA-256、直接 import、迁移决策和范围；所有 `migrated` 均为 false。不把待迁移代码伪装成已实现。完整文件用于校验；实际迁移时还需记录所选函数/片段及修改后摘要。

## 1. 第一批接入矩阵

以下为固定上游源码的端点约定，不代替厂商实时验证。P1 先实现官方按量 API Key + Chat Completions 上游，客户端 Messages/Responses 继续走本项目已有转换；不宣传厂商原生三协议全部接通。

| 平台 / 包名 | 固定官方 Chat 端点 | 目录探测端点 | 余额 | 来源 ID 模式 |
|---|---|---|---|---|
| DeepSeek / deepseek | `https://api.deepseek.com/v1/chat/completions` | `https://api.deepseek.com/v1/models` | `/user/balance`，多币种分别保留 | `deepseek/cn/{原始型号}` |
| 智谱 / zhipu | `https://open.bigmodel.cn/api/paas/v4/chat/completions` | `https://open.bigmodel.cn/api/paas/v4/models` | 本批未知/未提供 | `zhipu/cn/{原始型号}` |
| Kimi / kimi | `https://api.moonshot.cn/v1/chat/completions` | `https://api.moonshot.cn/v1/models` | `/v1/users/me/balance`，CNY | `kimi/cn/{原始型号}` |
| MiniMax / minimax | `https://api.minimaxi.com/v1/chat/completions` | `https://api.minimaxi.com/v1/models` | 本批未知/未提供 | `minimax/cn/{原始型号}` |

目录列是 P1 应验证的目标，不表示这四个接口当前均已实测可用。不支持目录的厂商须核对官方型号文档，采用有出处的有限列表；401/403/429/5xx 不能被静态列表掩盖。P0 未使用真实 API Key。

凭据拟定为平台私有 JSON：`api_key`、可选 `name`、`scope=cn`、`auth_mode=api_key`、`account_mode=payg`。API Key 为 NonExpiring，不伪造过期时间/refresh token；不要求各厂商密钥都以 sk- 开头。输入中冲突的认证模式应明确报错。

本批只交付国内官方端点，不开放任意 base_url，也不把国际站密钥送到国内端点。后续国际范围使用 global；P4e Coding Plan 使用 cn-coding / global-coding，与 payg 来源隔离。既有 payg 的 cn ID 不重命名、不自动把旧密钥改成订阅。相同原始型号仅在已有精确型号身份规则确认后合并，不将 deepseek-chat 等滚动别名猜成固定版本。

## 2. 精确复用决策

下面路径相对上游 `backend/internal/service/`；完整路径和摘要见清单。

| 文件/函数 | 决策 | 本地落点与必要改动 |
|---|---|---|
| domain_constants.go；provider_profile.go 的四个 payg 分支 | 抽取端点定义 | 四个平台包的官方端点常量；不用完整 ProviderProfile/ProtocolRule/Account 体系 |
| provider_profile_test.go；cn_providers_test.go 的 payg 分支 | 直接保留测试输入和断言含义，适配测试夹具 | 各平台 client_test；不引入 Gin/AccountRepository |
| cn_provider_balance_service.go 的 Kimi/DeepSeek switch；cnBalanceURL | 抽取余额协议解析 | 各平台 Credit；用现有 accounts.Credits 缓存与持久化路径，删除仓库写入/singleflight/proxy 依赖 |
| cn_provider_balance_service_test.go | 迁移非法/缺失/空数组与真实零余额用例 | 本地 httptest；保留多币种和未知值区别 |
| cn_provider_quota_service.go 的 cnParseF64 | 仅作为可复用的纯数值助手候选 | 若抽取则拒绝 NaN/Inf；不引入整个 Coding Plan 服务；其余文件留 P4e |
| ratelimit_cn_providers.go 的余额不足/频率特征判断 | 抽取纯判断与测试 | 平台错误映射；不复制 SetTempUnschedulable、周期余额检测或 RateLimitService |
| upstream_models.go 的 buildOpenAIAPIKeyModelsRequest / buildOpenAIModelsURL / 目录解析 | 抽取必要端点和解析规则 | 使用 providerhttp 与现有 catalog；不搬 AccountTestService、共享模型仓库或第二套元数据缓存 |
| openai_gateway_responses_chat_fallback.go 的 DeepSeek reasoning 历史规则及对应测试 | 有条件抽取 | 优先真实 reasoning_content 透传；不能将填空格宣称为恢复思维，不导入上游摘要/缓存服务 |
| openai_usage_deepseek_cache_test.go | 迁移缓存用量优先级测试向量 | 核对现有协议计量；上游有 unit build tag，迁移后不得因标签漏跑 |
| cn_provider_probe_url.go 及测试 | 参考风险和断言，不复制配置体系 | 固定端点 + providerhttp 禁止重定向；验证带密钥请求不会跟随 Location 外发 |
| account.go、openai_gateway_service.go、openai_gateway_chat_completions*.go | 仅参考 | 使用本地账号/网关/协议；不复制 Account、Gin、仓库、并发与重试所有者 |
| openai_gateway_request_body.go；openai_gateway_deepseek_input_image_test.go | 暂缓原生 Responses 部分 | 后续需要时再迁移；不能据测试文件名宣布所有型号具备视觉能力 |

“直接复制”针对可独立的规则、测试向量和函数，不等于复制整套服务。纯规则仍保留 LGPL 来源；改包名/改参数不消除许可证义务。

### 已发现的不能盲抄之处

- Kimi 余额分支对缺失/错误的 available_balance 忽略解析失败，会变成 0；本项目需明确未知，不能移植这个行为。
- cnParseF64 接受字符串浮点而不检查有限值；迁移时补 NaN/Inf 拒绝测试。
- DeepSeek reasoning 缺失时，上游补单个空格。真实内容必须优先保留；该兼容行为须经过目标协议测试，不应全平台开启。
- 完整余额服务调用 Repository.UpdateExtra，并有独立 singleflight/context.Background；本项目已有 Credits/Store，不能再带入一套写入和生命周期。
- 上游额度服务含 cc-switch 参考说明。P1 不迁移 Coding Plan 解析；P4e 实际选择片段时继续核对来源与许可证，不能只保留 Sub2API 名称就认为来源链完整。

## 3. 本地接入清单

| 现有位置 | P1 必需改动 |
|---|---|
| backend/internal/{deepseek,zhipu,kimi,minimax}/ | 实现现有 Adapter；厂商规则独立，标准传输使用 providerhttp；只有实际重复才抽小助手 |
| backend/internal/provider/provider.go；internal/app/app.go | 平台常量及注册；不新增生产空适配器 |
| internal/accounts/provider 登录与凭据协调 | 复用导入/预览/保存、静态密钥身份；API Key 验证格式与上游可用性明确分开 |
| internal/catalog/default_models.go | 目前预设 Providers 显式列九个平台；补四平台并测首次发现/追加/停用/ACL，不只做注册 |
| frontend/src/features/accounts/providers/controller.js、账号创建组件、accounts/controller.js | 现有标签/地区/导入提示存在硬编码；补 API Key 表单、平台名及范围，browser_login=false，不展示虚假 OAuth 按钮 |
| tools/check_architecture.py | PLATFORMS 增加四平台并保持传递依赖限制 |
| internal/protocol、providerhttp、server 平台处理点 | 检查模型、用量、错误、权限转换；不让已有平台专属分支误作用于新平台 |
| Dockerfile；deploy/package_release.py；deploy/package_windows.py；NOTICE | 实际复制代码时同步添加来源/许可文件，不能只改源码 |

P1 必测：四厂商端点与 Bearer 头、跨域重定向、错误帧/空响应/截断、取消释放、工具历史及大整数、缓存/推理计量、余额未知/零/多币种、保存失败、无浏览器登录按钮、自动共享名和分组隔离。目录成功不代替生成成功。不同 API 路径能否支持图片以实际模型/来源为准。

## 4. 许可证与分发记录

固定提交根 `LICENSE` 为 LGPL-3.0；根 `COPYING` 在该提交不存在（GitHub 404，树清单也未列出）。原计划暗示上游同时提供 COPYING 不准确，已更正。

本轮只有本地撰写的审查说明和文件摘要，没有新增编译进产品的上游代码。P1 复制时必须：

1. 在复制文件保留原版权/来源声明，记录上游 SHA、原始片段摘要、本地变更；不能覆盖为 Apache-2.0。
2. 迁移包携带 LGPL LICENSE 及所需 GPL-3.0 文本。仓库已有 modelmeta/COPYING 可作为待比对文本来源，但不能称其来自本轮上游根目录。
3. Linux/Windows/Docker 都附来源与修改记录、许可文本；源码提供全部修改和可重建命令，以支持用修改后的 LGPL 部分重建。具体组合与分发要求在真实代码迁移时按所复制部分核对，本审查不作“一定合规”的保证。
4. 检查原文件是否存在额外作者/第三方说明；有来源链疑问的片段暂不迁移，其他独立代码继续。

现有打包只显式附带 modelmeta/responsescompat 的来源材料，新平台不会自动被收录。P1 必须补打包测试；P0 不需要生成或发布新制品。

## 5. 回归基线与阶段完成条件

[验收记录](../../acceptance/SUB2API-PLATFORM-P0-2026-11-15.md) 记录本轮完整检查、九平台无缓存回归、范围和限制。

- [x] 固定 SHA，下载并逐项登记第一批候选源码与测试（22 文件）。
- [x] 明确抽取/参考/延期、依赖边界、许可证来源及后续制品要求。
- [x] 明确四平台 API Key/payg 范围、来源 ID、目录与余额策略。
- [x] 找到本地注册之外还需修改的目录预设、前端、架构检查与打包点。
- [x] 当前九平台基线通过；无生产服务/凭据/状态变更。

P0 完成不等于四个平台已接入。下一阶段 P1 按本清单实现四个官方 API 平台。真正复制后的文件/片段摘要、适配测试和真实密钥验收，均属于 P1，不提前勾选。

## 6. 复核来源

在独立临时目录下载固定版本（无需读取本机账号状态）。例如：

```bash
gh api 'repos/Wei-Shaw/sub2api/contents/backend/internal/service/provider_profile.go?ref=530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484' \
  -H 'Accept: application/vnd.github.raw+json' > /tmp/provider_profile.go
sha256sum /tmp/provider_profile.go
```

按清单 path 下载其余文件，对比字节长度与 sha256；不得用 main 的变化冒充固定版本一致。清单记录整文件的直接 imports，不声称是 Go 完整传递依赖图；已选迁移片段不依赖上游服务所有者。
