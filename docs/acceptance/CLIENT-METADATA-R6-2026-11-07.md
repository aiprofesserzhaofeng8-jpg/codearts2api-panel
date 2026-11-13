# R6：客户端参数与模型用途验收

日期：2026-11-07。

结论：R6 功能开发与本地验收完成。本次隔离候选快照的 89 个公开名称均有用途与导出结论；15 个满足导出条件，Pi 逐项调用 8 个通过、7 个 Kiro 来源受阻。60 项未生成参数是隔离快照的结果，包含原始 profile 读取受限的信息缺口，不是生产环境逐项核实后的缺参数量。因此按计划的完整真实验收口径，R6 仍为部分完成，不能把配置生成成功算作推理成功。生产服务未部署、源码未推送 GitHub。

## 实现与边界

- 在原有版本化 default_models.json 中增加少量精确来源的用途与实测建议，目录版本更新为 6。没有新增模型注册中心、运行时数据库、后台探测、权限缓存或推理时网络查询。
- 推荐参数优先采用已保存策略，其次采用上游目录；目录缺参时，只为已有精确来源证据的条目补充建议。原始 provider.Context / MaxOutput 保持原值。已保存策略的 Source 说明继续保留；它可能来自管理员手工配置或此前显式应用的建议，不冒充官方规格。
- 官方规格、平台目录数值、已保存策略与实测建议分别表达。官方原文标注的 1M / 128K 保留为描述标签，不自动转换为每个反代来源的可用数值。共享建议继续取全部启用且获授权来源的保守交集；来源缺参、缺目录或已审核 Qoder 槽位身份变化时，不能借其他来源提高建议。
- 模型用途包括聊天、平台自动路由、补全、跳转、重写、图片生成、平台智能体、摘要及用途未确认。chat 表示现有聊天适配器公布的入口，不认证底模身份或图片/工具能力；auto 明确提示实际底模可能变化。四个内部入口保留用途未确认。
- 同步 /v1/models、/v1/client-profile、管理员来源详情、策略表格和 Pi 导出。client_compatibility 表达 purpose、exportable、status、reason 及参数依据。专用入口不自动导出到聊天客户端；目录可见性、显式网关调用及原子权限复核保持原规则。
- 精确来源证据仅在管理员详情展示；共享名称的公开接口只提供公共建议和汇总依据，不泄露内部来源 ID。公开接口无参数时返回能力与原因，省略数值；不把未知或未确认用途伪造成可用配置。
- Pi 同步仍只获取一次模型目录，不逐模型查询或执行测活。保留其他 provider、密钥、基础地址、默认模型及未被目录覆盖的已有条目；未知参数保留已有值并报告，专用入口不自动新增，也不删除已有配置。已知参数同步时同时更新该模型的压缩预留/保留值，分别备份 models.json 和 settings.json；模型文件写入失败时恢复本次设置写入，避免留下部分更新。

管理员详情中的推荐参数不会自动改变推理请求、换成别的原生型号、扩大重试次数、清除暂停或改写预算。客户端导出条件与 R5 的健康、额度观测保持独立。

## GLM-5.3-Flash 的来源建议

智谱官方 [GLM-5.3-Flash/FlashX 规格](https://docs.z.ai/guides/vlm/glm-5.3-flash) 当前标注 1M 上下文、128K 最大输出。它描述原生模型，不能直接作为 CodeArts、Qoder 或 Trae 入口的数值保证。[WorkBuddy 模型说明](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Model) 也说明上下文档位应以其实际界面为准。

本次为三个原始入口各发送一次合成长输入，使用 4096 的输出请求预算，要求返回随机校验串：

| 原始入口 | 上游报告输入 Token | 实际输出 Token | 结果 |
| --- | ---: | ---: | --- |
| qoder/cn/gfmodel | 40140 | 172 | PASS |
| trae/cn/glm-5.3-flash | 40045 | 43 | PASS |
| codearts/glm-5.3-flash | 40044 | 52 | PASS |

因此为这三个精确来源提供 32768 上下文、4096 输出的保守客户端建议。测试确认该次输入规模和输出请求参数被接受，没有实际生成 4096 Token，也没有证明最大上下文或输出上限。Qoder 仍要求目录显示名匹配 GLM-5.3-Flash；目录改成其他型号后不继续套用建议。目录明确更小的限制优先，缺失目录不获得补充。

WorkBuddy 已有目录建议，本次不覆盖。四来源共享名称 glm-5.3-flash 的公共建议为 32768 / 4096；保留图片声明，压缩 reserve=16384、keep=8192。没有把全部未知模型统一填为 1M。

## 验证环境与结果

源码：/home/white/codearts2api-panel。系统服务仍在 127.0.0.1:10087 运行旧程序，本轮没有替换程序、重启或导入生产状态。

隔离候选网关使用当前公开名称快照、115 个来源的既有审计清单及已存在的客户端建议重建验收配置；89 个公开名称与当前列表逐项一致。生成请求通过旧网关认证后转发真实平台。因此验证覆盖新目录/参数投影、客户端导出和真实生成，但不代表复制了生产分组授权、管理员自定义来源关系或验证了所有原生适配器。尤其不能据此宣称 R5 的 Kiro 解析补丁已在线生效。

批量补充读取原始 profile 时遇到本机网关 HTTP 限流，96 项 GET 返回 429；这些响应没有被当成来源缺参或能力否定证据。最终验收使用已取得的共享建议与精确 Flash profile 补全隔离输入，未关闭生产限流；首份读取与初始夹具结果保留在私有产物中。

统计口径补充：未读取到的原始目录数值可能已经存在于生产服务，所以不能把下表的 60 项推广为当前生产的准确缺参数量。后续三项只读补查确认 workbuddy/cn/glm-5.3 的 profile 为 ready（1000000 / 8192），qoder/cn/dfmodel 和 teleagent/cn/chat-pro 为 missing_limits。该补查不改动配置，也不重新证明其余来源的上限或共享策略。正式生产数量仍需在遵守限流的条件下补齐数据后登记。

| 项目 | 结果 |
| --- | --- |
| 公开名称用途与参数结论 | 89/89 有明确结论 |
| 可自动导出的共享名称 | 15 |
| 缺少可靠参数 | 60；不新增猜测参数，已有 Pi 条目保留 |
| 专用或用途未确认入口 | 14；不自动新增聊天配置 |
| Pi 逐项真实短请求 | 8 PASS、7 BLOCKED；关闭客户端重试，不以退出码 0 代替回答检查 |
| 共享 Flash 启动器生成配置 | PASS，32768 / 4096，text + image，配置仅引用环境密钥 |
| 共享 Flash 的 Pi 直接识图 | PASS，随机文字与颜色识别正确 |
| 共享 Flash 的 Pi read 工具闭环 | PASS，读取合成文件并返回随机内容 |
| 现用 Pi 配置的直接调用 | PASS，直接连接现有 10087 服务并返回校验串 |
| 前端 | 59 项测试通过；真实管理 API 浏览器验收通过，覆盖用途/依据、桌面/移动端、测活与权限草稿 |
| 启动器 | 18 项测试通过，包含用途筛选、未知参数、其他 provider/默认值保留、压缩设置和写入失败恢复 |
| Go 与契约 | 完整 make check、catalog/config/server 专项 race、生成类型同步通过 |

7 个 BLOCKED 名称均走 Kiro：claude-haiku-4.5、claude-sonnet-4.5、deepseek-3.2、kiro-auto、minimax-m2.1、minimax-m2.5、qwen3-coder-next。配置已进入真实请求，旧网关返回 502；不得因此推断模型不支持聊天，也不能隐藏这些名称来把通过率改成 100%。Kiro 故障继续按 R5 的真实阻塞处理。

本次未做上下文极限、持续输出极限或长程自动压缩的真实压测；没有为每个型号重复图片与工具验证。新增 Flash 的直接图片和工具用例具有单独证据。

## 本地修改与备份

- 修改前源码与 Pi 文件备份：/home/white/backups/codearts2api/before-r6-20261107-200452/。
- 现用 /home/white/.pi/agent/models.json 新增 glm-5.3-flash，text + image，32768 / 4096；其他模型、provider、凭据和基础地址逐项核对保持。
- 现用 settings.json 仅新增 codearts-local/glm-5.3-flash 的 reserve=16384、keep=8192；默认仍为 codearts-local/workbuddy/cn/glm-5.3-flash，其他设置保持。
- 两份现用配置另有相邻的 before-r6 私有备份，具体路径记在 artifacts/r6-clients-2026-11-07/active-pi-update.json。
- 源代码、本地配置与真实测试产物分开保留；私有产物和密钥不提交 Git。未推送或发布 Release。

私有证据目录：artifacts/r6-clients-2026-11-07/。完整检查日志：/tmp/r6-check-complete.log；专项 race：/tmp/r6-race-final.log；浏览器：/tmp/r6-browser-final.log。

## 89 个公开名称的逐项结论

后续策略更新：按用户要求直接复用 Sub2API 的缺项合并和公开资料解析，现有聊天来源的缺参数现在可使用明确标注的参考资料或默认模板，不再一律阻止配置。下表保留本轮原始验收口径，不能当作后续版本的当前结论；见 [后续参数复用验收](SUB2API-MODEL-METADATA-REUSE-2026-11-07.md)。

以下是本次隔离候选快照的导出/用途结论。缺参和专用入口已经有处理结论，不表示其真实平台调用已经验证。

| 公开名称 | 用途 | 推荐上下文 / 输出 | 本次结论 |
| --- | --- | --- | --- |
| chatgpt-codex-auto-review | 专用智能体 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| chatgpt-gpt-reserve | 用途未确认 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| claude-haiku-4.5 | 聊天 | 200000 / 8192 | BLOCKED：Kiro 502 |
| claude-sonnet-4.5 | 聊天 | 200000 / 8192 | BLOCKED：Kiro 502 |
| codearts-speed | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| deepseek-3.2 | 聊天 | 164000 / 8192 | BLOCKED：Kiro 502 |
| deepseek-v4-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| deepseek-v4-flash-official | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| deepseek-v4-pro | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| deepseek-v4-pro-official | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| deepseek-v4.1-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| deepseek-v4.1-flash-1017 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| doubao-seed-2.0-code | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| doubao-seed-2.1-pro | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| doubao-seed-2.1-turbo | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| glm-5 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| glm-5-turbo | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| GLM-5.2 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| glm-5.2-sft-harmony | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| glm-5.3 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| glm-5.3-flash | 聊天 | 32768 / 4096 | Pi PASS |
| glm-5.3-flashx | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| gpt-5.6-luna | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| gpt-5.6-sol | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| gpt-5.6-terra | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kimi-k2.6 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kimi-k2.6-vl | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kimi-k2.7-code | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kimi-k2.8-preview | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kimi-k3 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kimi-k3-1 | 聊天 | 1000000 / 8192 | Pi PASS |
| kimi-k3.1-pro | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| kiro-auto | 平台自动路由 | 1000000 / 8192 | BLOCKED：Kiro 502 |
| mimo-v2.6-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| mimo-v2.6-pro | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| minimax-m2.1 | 聊天 | 196000 / 8192 | BLOCKED：Kiro 502 |
| minimax-m2.5 | 聊天 | 196000 / 8192 | BLOCKED：Kiro 502 |
| minimax-m3 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| minimax-m3.1-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| openpangu-2.0-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| openpangu-2.0-pro | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| qoder-auto | 平台自动路由 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| qoder-fast | 平台自动路由 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| qwen-3.7-plus | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| qwen3-coder-next | 聊天 | 256000 / 8192 | BLOCKED：Kiro 502 |
| Qwen3-VL-235B | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| qwen3.8-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| qwen3.8-max | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| step-5-preview | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| teleagent-chat-flagship | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| teleagent-chat-flash | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| teleagent-chat-lite | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| teleagent-chat-nano | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| teleagent-chat-pro | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-aquila | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-browser_use_subagent | 专用智能体 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| trae-computer_use_subagent | 专用智能体 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| trae-custom_model_1M | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_1M_text | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_claude | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_deepseek_chat | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_deepseek_reasoner | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_deepseek_v4 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_doubao_1M | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_doubao_256k | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_gemini | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_gpt-5 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_gpt-6 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_kimi | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_no-fc | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-custom_model_placeholder | 用途未确认 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| trae-Doubao-Seed-Evolving | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-explore_sub_agent_v2 | 专用智能体 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| trae-file_search_agent | 专用智能体 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| trae-sagitta | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-seed-code-pro-0430 | 聊天 | 未知或不适用 | 不新增配置：缺少可靠来源参数 |
| trae-summary | 摘要 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-auto | 平台自动路由 | 256000 / 8192 | Pi PASS |
| workbuddy-codewise-completions | 补全 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-codewise-default-model-v2 | 用途未确认 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-codewise-jump | 跳转 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-codewise-rewrite | 重写 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-hunyuan-image-alpha | 图片生成 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-hy3 | 聊天 | 192000 / 8192 | Pi PASS |
| workbuddy-hy3-x | 聊天 | 192000 / 8192 | Pi PASS |
| workbuddy-hy4free | 聊天 | 1000000 / 8192 | Pi PASS |
| workbuddy-hy4std | 聊天 | 1000000 / 8192 | Pi PASS |
| workbuddy-nes-gf | 用途未确认 | 未知或不适用 | 不自动导出：专用或用途未确认 |
| workbuddy-space-bunny | 聊天 | 1000000 / 8192 | Pi PASS |
