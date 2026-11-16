# P4e 国内 Coding Plan 接入

日期：2026-11-16。状态：Kimi、智谱个人版和 MiniMax 国内套餐路径已实现；真实套餐验收待完成，MiniMax 当前额度响应结构待实测，P4e 不标记真实验收完成。TypeSafe 独立目录/页面工作暂缓，不作为此阶段前置条件。

## 已核对的范围

固定 Sub2API 提交 530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484 的 account_kimi_coding_test.go 与 cn_provider_quota_service.go 确认 Kimi、智谱、MiniMax 的 Coding 模式；没有据此新增 DeepSeek Coding 模式。额度映射复用记录在 internal/cnapi/UPSTREAM.md 和 upstream.json。

Kimi 官方文档：https://www.kimi.com/code/docs/ 。国内 Coding OpenAI 兼容地址为 https://api.kimi.com/coding/v1，普通 Moonshot 为 https://api.moonshot.cn/v1；密钥和计费模式不能混用。

## 本批实现

- 现有 Kimi 平台添加接入套餐选项：Moonshot 按量付费 / Kimi Coding。仍复用原账号导入、目录刷新、共享名称、权限、准入和账本。
- 普通账号保留 cn/payg 及原身份摘要；Coding 使用 coding-cn/coding，摘要包含来源范围。相同字符串密钥不会让两种模式互相覆盖，明确冲突的 scope/mode 拒绝导入。
- cnapi 增加一个可选固定 Coding profile，按凭据选择；不新增平台注册项、数据库、缓存、后台任务、路由队列或失败回退模式。
- Coding 的 /models、/chat/completions、/usages 均使用同一个固定 Coding 地址。客户端 Chat/Responses/Messages 复用现有转换；本批不是上游原生 Responses 透传。
- /usages 复用 Sub2API 的 limits[].detail 五小时、usage 周窗口字段映射；数值缺失保持未知，零剩余可正确显示，不伪造美元余额。不在推理请求前查询额度。
- 不把 kimi-for-coding 等动态套餐别名强行合并为某个固定原生版本。明确型号沿用已有共享名称规则。
- 不伪装 Claude Code 或 Kimi CLI User-Agent；没有真实账号授权/生成证据。

## 隔离验收

- 单元及竞态：套餐选择、旧身份稳定、跨模式隔离、非法 URL/冲突模式、目录来源、生成和余额路径、401/403/429/500 不回退、缺失/非法/零额度。
- 网关矩阵：Coding 来源加入原官方 API 集成用例，覆盖三个客户端协议、流式/非流式、原始/共享名称、用量/缓存 token、分组拒绝和租约释放。
- 浏览器：中英文的套餐选择、导入数据、保存失败后重试、成功后清空密钥；原五平台导入回归通过。
- 日志：/tmp/codearts-p4e-race.log；/tmp/codearts-p4e-browser/browser-results.json。
- 全量 make check 通过：/tmp/codearts-p4e-check.log。Linux 测试程序 /tmp/codearts-p4e-panel 已构建，未安装。早期 cnapi 测试仍将全部 coding 模式视为非法，已按新增 Kimi 支持修正，其余平台继续拒绝。

## 第二批：智谱个人 Coding / MiniMax M Plan

- 沿用 cnapi 固定 profile；三平台均为 coding-cn/coding，与既有 cn/payg 分离。同一账号表单增加套餐选择，不增加页面、目录、缓存或后台任务。
- 智谱 Chat 使用 https://open.bigmodel.cn/api/coding/paas/v4；额度使用固定 /api/monitor/usage/quota/limit，裸 API Key 鉴权。团队组织/项目字段本批不接受，避免静默忽略后误查个人额度。
- 智谱按 TOKENS_LIMIT 优先、CREDIT_LIMIT 备选，unit=3/6 区分五小时/周窗口。未知 unit 保留普通窗口名称，不按重置时间推断；缺 percentage 不当作 0%。
- MiniMax 官方当前名称为 M Plan，Chat 仍为 https://api.minimax.cn/v1；按当前官方 FAQ 使用 https://www.minimax.cn/v1/token_plan/remains。已知 general 桶字段复用 Sub2API，跳过 video 桶；只有 weekly_status=1 时展示已返回的周额度。新结构不识别时显示未知，不自动请求旧域名。
- 套餐备用目录：智谱沿用已声明 GLM 型号；MiniMax 仅列官方套餐确认的 M3.1 Flash，不把普通 API 全部型号默认视为套餐权益。目录 404/405 时用备用目录，并以一次短推理验证身份；401/403 不降级备用目录。实际型号是否可用仍由上游决定。
- 三个客户端协议仍复用既有 Chat 转换，不宣称上游原生 Responses/Messages 透传。

验证记录：定向竞态 `/tmp/codearts-p4e2-race-final.log`；中英文八种导入模式浏览器 `/tmp/codearts-p4e2-browser-final/browser-results.json`。覆盖模式/身份隔离、固定路径与两种额度鉴权、目录缺失时真实传输的模拟验证、错误不回退、三协议/共享名称/计量/分组限制及租约释放。

早期发现文件名 coding_windows.go 被 Go 视为 Windows 专用文件，已改为 coding_plan_quota.go；负数剩余百分比测试发现的问题已修正。最终检查结果以 final 日志为准。

## 尚未完成

1. 三个平台真实套餐账号验证；包括当前 MiniMax M Plan 用量响应字段，以及上游是否接受本网关客户端身份。
2. 智谱团队组织/项目、海外套餐本批未开放。
3. DeepSeek 未确认独立 Coding 套餐，不添加虚假选项。
4. 后续计划为 P5 Bedrock / Vertex AI；TypeSafe 专用页面不继续扩展。

未部署、未推送、未发布；未使用生产凭据和状态。真实可用性不以模拟通过代替。

最终前端提示已修正为支持三平台套餐，重新构建并通过浏览器回归及静态资源/CSP 检查。Linux/Windows 构建产物为 /tmp/codearts-p4e2-panel 与 /tmp/codearts-p4e2-panel.exe，均未安装。

第二批修正后全量 make check 通过：/tmp/codearts-p4e2-check-final.log。此前失败日志由最终全量与定向竞态结果取代。
