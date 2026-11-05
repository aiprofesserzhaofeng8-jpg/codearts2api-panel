# 统一模型迁移与验收矩阵

日期：2026-11-05。状态：U1–U6 已完成，以下是实施契约和验收矩阵；执行证据见 [U6 验收](../acceptance/UNIFIED-MODELS-U6-2026-11-05.md)。字段与 API 见 [契约 v1](UNIFIED-MODELS-CONTRACT.md)。

U2 已实现迁移核心、预检查/离线命令及显式受管升级路径，具体已执行范围见 [U2 验收](../acceptance/UNIFIED-MODELS-U2-2026-11-05.md)。下文是完整 U2–U6 矩阵；隔离路由、Pi 和包升级恢复已验收；[后续真实验证](../acceptance/UNIFIED-MODELS-LIVE-2026-11-05.md) 已覆盖 WorkBuddy/Trae 的 deepseek-v4.1-flash，实际部署没有验收。

## 1. schema 10 -> 11 的固定转换

迁移核心置于现有 Go 存储边界，供启动加载和受限迁移命令复用。部署 Python 工具只做校验、停写、备份、调用及切换，不重复解释模型授权。新二进制提供只读 `-check-state-migration <state.json>`，输出脱敏 JSON；这是当前候选程序提供的只读命令。

只读输出固定包含 `from_schema`、`to_schema`、`ready`、`definitions`、`group_changes`、`conflicts`、`warnings`、`routing_input_sha256`。摘要仅覆盖去凭据的路由配置，不输出原文件摘要、账号明文、密钥或密码。conflict 每项为 `{reason,group_ids,model_name,field_path,message}`。退出码 0=可迁移/已是目标，2=需要解决配置冲突，1=输入/文件错误。只读检查不能调用普通 `Open` 的自动 Save、导入、续期、领奖和后台循环。

实际离线转换命令定为 `-migrate-state-dir <input-dir> -migration-output-dir <new-dir>`：输入必须是完整且已停写的数据目录，输出必须不存在；复制现有全部受管 sidecar，执行同一转换与 journal 校验，生成 schema 11 新目录。它不替换输入、不启动后台任务、不调用上游。失败保留有阶段标记的隔离输出供诊断，再运行必须另选新目录；不能把不完整目录当成功结果。拒绝符号链接、路径重叠和可识别的活跃写进程；无法验证共享文件系统其他写者，部署约束仍为单实例。

转换步骤：

1. 对版本和旧字段进行严格解码，使用现有 schema 10 规范形状。早于 10 的状态先通过既有迁移在独立新目录升级到 10；本次受管自动流程仅接受 10→11。高于 11 拒绝。
2. 收集所有组映射，去除 source 的比较顺序差异但保留平台型号大小写。规范化 CodeArts source 为完整 ID；其他保存过的 ID 原样保留，不能离线猜不存在的型号。
3. 以公开名称按大小写折叠分桶。只有精确同名、规范来源集合完全相同才合并；大小写拼写不同、同名不同来源均是冲突。检查内置别名、自定义别名和当前已知原始短名冲突。
4. 每个无冲突定义 ID 固定为 `um_` + SHA-256(`UTF8(name)`) 的前 32 个十六进制字符，revision=1、enabled=true；这是迁移专用确定性 ID，创建新定义仍用随机 ID。检查截断碰撞，碰撞作为冲突停止。源默认 enabled=true，按 ID 排序，不增加原本未配置的来源。
5. 每个原映射生成固定 `selected` grant。来源用原始列表，不去掉目前禁用、无账号、目录缺失或被原始规则拒绝的项。grant visible/callable 从原公开名规则取值，缺省均 true。
6. 原有 model_rules 中公开名规则移入 grant，其余保留为原始规则；原始 model_ids、allowlist_enabled、account_ids 不变。有映射旧组设置 qualified_only/false；无映射旧组设置 legacy/true 且 grants=[]。所有组 revision=1。
7. 默认模型和客户端偏好保持原名称，不自动换版本；已失效的默认引用在预检查中报冲突，需要显式清除/修正。没有配置默认值时保持空。ForceModel 与任何待迁移定义共存报冲突。
8. 全局 ModelPolicies、Prices、ModelHealth、用户/密钥绑定和账本键保持原样；不创建统一策略覆盖。公共 profile 后续从原始策略合并。初始化 routing revision=1，移除旧 model_mappings，设置 version=11。
9. 完整候选状态校验成功后才按已有私有文件/原子提交机制落盘。Save 失败不发布新对象，升级副本保持可诊断；对 schema 11 重跑是 no-op，不能重新生成 ID 或重置 revision。

离线检查可确定已保存 ID、CodeArts 内置原始目录和内置别名、自定义别名键。运行时不在磁盘保存的目录不假装已完整获取；报告 `catalog_not_verified_offline`。旧缺失来源保留，启动后 Registry 只解析确实存在的原始 ID；新出现的裸名冲突按契约只通过完整原始 ID 调用，不能抢占公开名称。U2 必须把这个限制写入预检查输出，不能靠读取上游凭据补齐。

## 2. 冲突处理和受管升级

### 2.1 处理原则

- 同名不同来源：管理员先在旧实例显式重命名某组映射，或确认业务上要统一后手工调整旧配置，再重新预检查；工具不能替其合并授权。
- 原始短名/内置别名冲突：为统一模型选择不冲突、带版本的公开名，关联默认值和规则一起修改；内置别名不能以删除源码方式绕开。
- 无账号或目录暂缺是 warning，不是自动换型号依据。没有任何账号仍可转换配置，真实调用按 503 处理。
- 迁移报告仅描述结构；真实账号、日志内容和调用密钥不进入 Git 或报告。

### 2.2 受管升级阶段

现有 `deploy/panelctl.py upgrade` 在相同 schema 检查后才停服务。当前已提供显式 `--migrate-schema`，候选版本只声明支持 10→11 时可用；无该参数或不在支持集合的跨 schema 升级仍拒绝。

1. 校验发布包、摘要、版本、平台及迁移能力元信息；调用候选二进制只读预检查。原服务此时继续运行，报告不是最终写入许可。
2. 冲突直接结束，旧服务不停止。检查通过后停写、确认服务进程与目录无写者，完整备份数据和账本；再次对停止后的快照执行相同检查，配置摘要变化时以最后快照为准。
3. 调用候选二进制的离线转换命令生成隔离迁移目录，并验证 journal 重放，原目录继续保存升级前状态。验证配置/凭据不变、只结算已有 checkpoint 后尚未结算条目，不重算历史价格。
4. 候选数据完全写好并 fsync 后，通过部署工具保存恢复标记、原数据位置和阶段。依次切换数据目录和版本链接；这两步不是一个原子操作，所有入口遇到未完成标记必须进入恢复流程，不能启动错版本/错目录。
5. 启动并核对版本、schema、路由定义和健康。启动可能续期/领奖或产生新账本；因此切换后异常停止候选并保留最新数据，不自动恢复旧账号快照。切换前失败可直接恢复原服务。
6. U6 已以 `recover-schema` 模拟每个切换阶段的崩溃后磁盘布局，并用真实程序执行有界、可重复的向前恢复。恢复标记存在时禁止猜测“最近目录”自动覆盖。只在证明无新写入的隔离实例中演练整份旧备份恢复。

以上是升级实现契约，不授权本轮操作本机真实状态。源码启动也使用同一迁移校验：冲突在后台启动前失败，不接受忽略权限字段的旧读取路径。Docker/手工部署必须先停写并在副本预检查，再替换受管数据，不执行两个写进程。

## 3. 权限真值表

约定 U 为已知统一模型，A/B 为其配置来源，g 为组，raw ACL 指原始型号白名单与 callable；无账号、冷却与能力在权限之后判断。以下每行都要有 U2/U3 自动化用例。

| ID | 输入条件 | 列表结果 | 推理结果 |
| --- | --- | --- | --- |
| P01 | 不分组，U 启用，A 可用 | 显示 U | 可选 A |
| P02 | 不分组，U 启用，无账号 | 显示 U | 503 sources_unavailable |
| P03 | 有组但没有 U grant | 不显示 U | 403 permission_denied |
| P04 | grant visible=true/callable=true，selected=[A] | 显示 U | 仅可选 A |
| P05 | visible=false/callable=true，A 可用 | 不显示 U | 显式 U 可调用 |
| P06 | visible=true/callable=false | 显示 U，profile null | 403，不访问 A/B |
| P07 | visible=false/callable=false | 不显示 U | 403 |
| P08 | selected=[] | 若 visible=true 则显示 | 403，不能按 all 理解 |
| P09 | selected=[A]，原始白名单只允许 B | 显示 U | 403 |
| P10 | selected=[A]，A 原始 callable=false | 显示 U | 403 |
| P11 | selected=[A]，A 原始 visible=false、callable=true | 显示 U | 仍可选 A |
| P12 | selected=[A]，A 故障但 B 正常 | 显示 U | 503 或 A 的执行错误，不越权到 B |
| P13 | all，source_ids=[]，新增合法 C | 显示 U | 可选 C，但仍要有组内对应账号与 raw ACL |
| P14 | selected=[A,B]，新增 C | 显示 U | 不能选 C |
| P15 | U 全局停用，grant 有权 | 不显示 U | 503；原始路径仍按其 ACL |
| P16 | A 已配置但目录缺失 | 显示 U，资料缺失时 profile null | 其他来源可用则选它，否则 503 |
| P17 | 有权，组内所有账号停用或 account_ids=[] | 显示 U | 503 |
| P18 | 原始完整 ID A，不存在 U grant | 依原始规则 | 依原始 ACL，不借 U grant 绕过，也不因缺 U 一概拒绝 |
| P19 | 原始裸名 A，raw_model_mode=qualified_only，无同名 U | 按原始可见规则显示完整 ID | 404，沿用旧有映射组的裸名限制 |
| P20 | 解析后撤销 U grant | 无须等缓存刷新 | 下一次准入/重试 403，无上游命中 |
| P21 | 解析后定义或模型策略 revision 改变，仍有权 | 下一次列表为新快照 | 409 configuration_changed，无额外尝试循环 |
| P22 | 有权且 A/B 均存在，只有 B 明确支持图片 | 公共 profile 不承诺全部支持 | 图片显式请求仅选 B；都 unknown 时 400 |
| P23 | A/B 全无账号，但来源策略完整 | 显示 U，profile 可返回 | profile 200 不代表推理可用；推理 503 |
| P24 | grant 只允许 B，但客户端明确指定原始 A | 依原始规则 | 若原始 ACL 明确允许 A 可调用，否则 403；公开来源限制不冒充原始 ACL |

P06 的 profile 直接请求为 403，列表嵌入 profile 为 null 并附 permission_denied。授权范围为空的 profile 同样 403；临时无账号不妨碍其参数读取。新组默认不开放未明确选择的统一模型。

## 4. 迁移检查清单

| ID | schema 10 输入 | 预期转换/冲突 | 实施验收位置 |
| --- | --- | --- | --- |
| M01 | 无任何映射 | 全局空目录，组 grants=[]，保留裸名称路径 | U2 storage |
| M02 | 一组 U->[A,B] | 一个定义、一个固定 grant、默认值不变 | U2 storage |
| M03 | 两组精确同名且来源集合相同，顺序不同 | 合并一个稳定 ID，两份独立固定授权 | U2 storage |
| M04 | 同名分别 [A] / [B] | name_sources_conflict，零写入 | U2 预检查 |
| M05 | 两组名称仅大小写不同 | name_case_conflict，不自行改名 | U2 预检查 |
| M06 | U 与 CodeArts 原始裸名或内置别名同名 | name_namespace_conflict，原路由不变 | U2 预检查 |
| M07 | U 与自定义别名冲突，即使目标相同 | name_namespace_conflict | U2 预检查 |
| M08 | 旧来源失效/账号删除或停用 | 保留来源绑定与固定列表，报告 warning | U2 storage |
| M09 | 全部账号为空 | 允许无冲突结构迁移，不标为可调用 | U2 storage |
| M10 | 原始白名单开启但来源不在其中 | 旧状态若已有效存储则保留限制，不能扩大；新编辑仍按契约拒绝不一致默认 | U2 权限对比 |
| M11 | 原公开名或来源 visible/callable 不同 | 公开规则移到 grant，来源规则保留；P05–P11 等价 | U2 权限对比 |
| M12 | 空白名单 + allowlist_enabled=true | 保持拒绝全部原始来源，不能改成不限制 | U2 storage |
| M13 | 已保存默认值不在映射或已禁止调用 | default_reference_conflict，先修配置 | U2 预检查 |
| M14 | ForceModel 非空且有映射 | force_model_conflict，零写入 | U2 预检查 |
| M15 | 来源超过上限、重复平台区域、非法 ID | invalid_legacy_mapping，不静默丢条目 | U2 预检查 |
| M16 | 二次迁移、重启 | schema 11 no-op，ID/revision 不重置 | U2 storage |
| M17 | 新目录以后出现与 U 同名的原始短名 | U 继续原含义，新原始只通过完整 ID | U3 Registry |
| M18 | 历史原始价格、策略、ModelHealth、用户/密钥归属 | 值及原始键不变；消费者钱包/预算不重置 | U2 数据往返 |
| M19 | journal 在 snapshot 之后还有已完成记录 | 依 checkpoint 只重放一次，费用不按公开名重算 | U2/U6 recovery |
| M20 | 保存失败、截断文件、非法 schema 或 ID 截断碰撞 | 明确失败，无半迁移发布 | U2 storage |
| M21 | 普通升级遇 schema 10→11，无显式迁移选项 | 停机前拒绝 | U6 deploy |
| M22 | 显式迁移已预检，但停写后配置发生变化 | 重新检查最后状态；新冲突则恢复原服务，不用旧报告放行 | U6 deploy |
| M23 | 版本链接/目录切换各阶段崩溃 | 按恢复标记继续/停止，禁止旧二进制读新 schema | U6 deploy |
| M24 | 旧版程序打开 schema 11 | 拒绝，不删 version 绕过 | U6 包验收 |

## 5. 原计划 T01–T16 的落地验收位置

| 原编号 | 固定输入/断言 | 对应阶段和现有测试位置 |
| --- | --- | --- |
| T01 | 两平台 A/B，同一 U；分别返回不同上游标记，外部 model=U | U3/U4 catalog、server/group_model_mapping_test.go |
| T02 | 不分组 key、U 已启用、同一原始账号集 | U3 catalog + storage/pool |
| T03 | P04/P12，B 可用但不在来源授权 | U3 原子准入 |
| T04 | P13/P14，新增 C 后比较命中记录 | U2/U3 权限更新 |
| T05 | P05–P11 的独立可见/调用组合 | U2/U4 access + API |
| T06 | M04–M07，状态摘要前后相同 | U2 迁移预检查 |
| T07 | P08 + null/省略/API更新 | U2/U4 分组契约 |
| T08 | P02/P15/P16/P17，无推理调用且诊断正确 | U3/U4 503 |
| T09 | P22；图片正文与工具 schema 大整数保持 | U3/U4 protocol + server |
| T10 | 一来源无 policy、矛盾大小限制、公共策略回退 | U4 client_profile_test.go |
| T11 | 3 协议 × 流式/非流式 × A/B | U4 集成；模型外壳改名不改工具参数 |
| T12 | P20/P21，解析与 Acquire 中间设置屏障修改 | U3 storage/gateway 并发 |
| T13 | 502 首段前/后、取消、限额；尝试数不超过原上限 | U3/U6 retry + ledger |
| T14 | M16/M19–M24，临时实例/失败注入 | U2/U6 storage + deploy |
| T15 | qualified_only/legacy、ForceModel、默认/偏好 | U3/U4 解析顺序 |
| T16 | 两写者读取 revision=1，依次提交不同 sources | U2/U4 一成功、一409，不丢更新 |

每个未来案例记录“输入、调用路径、预期、实际、上游命中次数/来源、状态差异”，真实测试额外记录版本、时间、额度。U1 只校验规格覆盖和文档一致性；不能将表格视为测试已通过。
