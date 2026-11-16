# 国内官方 API Key 接入

源码已实现 DeepSeek、智谱、Kimi、MiniMax 的国内官方按量 API；隔离测试通过，尚未使用真实官方密钥验收，也未部署到当前运行服务。订阅客户端账号、Coding Plan、国际站密钥不在本批范围。

## 添加账号

账号池 → 添加账号 → 选择相应的 `API` 平台 → 下一步 → 输入 API Key → 校验并导入。输入框隐藏密钥字符，保存失败保留表单以便重试，成功或退出时清空。

程序先检查凭据格式，再请求官方模型目录；智谱/MiniMax 目录返回 404/405 时，使用有出处的小型型号列表，并发送一次最多 64 输出 token 的短生成校验，可能产生少量按量费用。401/403/429/5xx 不会被备用列表掩盖，验证失败不会保存账号。目录可访问不代表每一个型号都有推理权限。

没有浏览器授权、没有假 refresh_token 或虚构过期时间。密钥失效时重新导入，勾选覆盖已有账号；账号身份来自平台隔离的密钥摘要，不是伪造官方用户 ID。

## 端点和余额

| 平台 | 固定 API 基址 | 目录 | 余额 |
|---|---|---|---|
| DeepSeek | https://api.deepseek.com/v1 | 动态 /models | /user/balance，多币种分别展示 |
| 智谱 | https://open.bigmodel.cn/api/paas/v4 | 动态优先；目录不可用时 GLM-5.3、GLM-5.3-Flash | 暂不支持查询 |
| Kimi | https://api.moonshot.cn/v1 | 动态 /models | /v1/users/me/balance，CNY |
| MiniMax | https://api.minimax.cn/v1 | 动态优先；目录不可用时 MiniMax-M3.1-Flash、MiniMax-M3 | 暂不支持查询 |

MiniMax 使用当前[官方 OpenAI SDK 文档](https://platform.minimax.cn/docs/api-reference/text-openai-api)的域名。智谱备用列表依据[官方模型概览](https://docs.bigmodel.cn/cn/guide/start/model-overview)。这些文档能力说明不等于本项目已用真实密钥验证图片识别或最大上下文。

余额缺字段、非法数字或错误响应报查询失败，不能显示成 0；不猜测总额、已用或统一货币。不同币种不相加，不作为新的调度限额。

## 模型与客户端

原始来源格式：`deepseek/cn/原型号`、`zhipu/cn/原型号`、`kimi/cn/原型号`、`minimax/cn/原型号`。保存后刷新目录，已有自动同步将确认的精确型号加入共享名称。滚动别名不猜成固定版本，分组权限不自动扩大，停用状态保留。

客户端继续使用本项目 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`，支持现有流式/非流式转换；上游本批统一使用 Chat Completions，不声称接通厂商原生 Responses/Messages。工具参数、reasoning_content 历史、图片内容块和用量原样传递，不替换模型，不丢弃输入，不缓存推理内容或上传文件。未知型号能力保持未知；已知智谱/MiniMax 型号能力使用明确官方资料。

使用 Pi 等客户端时选择共享名称，并确认 API Key 所属分组允许该来源。新平台上线后需要重新生成客户端目录，旧配置不会自动增加条目。

## 验证与限制

见 [P1 验收](acceptance/SUB2API-PLATFORM-P1-2026-11-15.md) 和 [Sub2API 复用来源](../backend/internal/cnapi/UPSTREAM.md)。模拟请求全部使用临时服务、虚构密钥；真实密钥无需粘贴到聊天，在面板输入即可。未触及当前生产账号、服务或 GitHub Release。

## Kimi Coding（P4e 首批）

添加 Kimi 账号时选择 Kimi Coding / Subscription，并填写套餐控制台的密钥；普通 Moonshot 账号保持 Pay as you go。套餐来源标识为 `kimi/coding-cn/<上游型号>`，不自动回退至按量付费。目录读取实际 `/models`，客户端仍调用现有三个兼容接口；额度显示五小时/周窗口。不要将动态 `kimi-for-coding` 别名手动认作固定模型版本。

该模式已通过隔离测试，尚未真实套餐验证；国内入口已实现，海外入口未开放。

## 智谱个人 Coding / MiniMax M Plan

在原有平台的账号导入表单选择 Coding Plan 或 M Plan，填入对应套餐密钥。来源范围为 `coding-cn`，普通 API 仍为 `cn`；不自动更换计费模式。MiniMax 使用当前官方 M Plan 用量地址，仅识别已确认的窗口字段，未知结构显示未知。智谱团队组织/项目和海外套餐未开放。上述路径为隔离验收，尚未真实套餐验证；完整状态见 P4e 验收文档。
