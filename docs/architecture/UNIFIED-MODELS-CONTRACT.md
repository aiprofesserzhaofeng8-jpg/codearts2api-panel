# 全局统一模型契约 v1

日期：2026-11-05。状态：U1 已冻结，U2–U6 已实现全局定义、推理、HTTP 契约和页面，并完成综合隔离交付验收。后续 [真实验证](../acceptance/UNIFIED-MODELS-LIVE-2026-11-05.md) 已覆盖 WorkBuddy/Trae 的 deepseek-v4.1-flash，其他平台/型号未验证；源码尚未部署到当前系统服务。决策依据为 [ADR 0018](../adr/0018-global-unified-models.md)。

阶段说明：U2 曾保留旧组 HTTP 兼容写入；U4 已切换为严格版本化授权，拒绝旧 model_mappings 字段。U5 已撤下旧映射编辑器，管理页改读规范授权；旧只读投影保留于历史内部兼容接口及测试夹具，不是第二份持久化定义。当前 HTTP 契约已合入 OpenAPI，见 [U4 验收](../acceptance/UNIFIED-MODELS-U4-2026-11-05.md) 和 [U5 验收](../acceptance/UNIFIED-MODELS-U5-2026-11-05.md)。

## 1. 数据字段

### 1.1 State 新字段

- `unified_models`：`UnifiedModel[]`，空数组表示未配置全局统一模型，最多 16,384 条，覆盖旧版 128 组 × 128 映射的上界。
- `model_routing_revision`：正整数，最大 `9007199254740991`。schema 11 初始化为 1；定义、策略、别名或全局模型默认/强制值成功改变时递增。健康/并发/额度/日志变化不递增。
- schema 11 不保存旧 `groups[].model_mappings`。不得同时读写两套定义。schema 10 仅在迁移输入解码器支持该字段。

schema 11 加载缺少这两个新字段、组 revision 或组兼容字段均视为损坏状态，不能用零值解释成全部放行；新建/迁移统一生成字段。空统一目录表示合法零定义。

### 1.2 UnifiedModel

| JSON 字段 | 类型与必填 | 语义 |
| --- | --- | --- |
| `id` | string，只读 | `um_` + 32 个小写十六进制字符，创建后固定 |
| `name` | string，创建必填、更新须相同 | 1–200 UTF-8 字节，无空白/控制字符/斜线；创建后固定 |
| `enabled` | bool，写入必填 | 全局调用开关；关闭后不删来源、不自动撤销分组引用 |
| `revision` | 正整数，只读 | 从 1 开始，每次成功的有效修改加 1；上限同 routing revision |
| `sources` | array，写入必填 | 1–16 项，每个平台/地区最多一项；顺序用于显示，不隐含优先级 |
| `sources[].id` | string，必填 | 原始来源完整 ID，1–256 字节；来源身份即此 ID |
| `sources[].enabled` | bool，必填 | 候选来源开关 |

示例只说明数据形状，不代表这些平台当前可用：

```json
{
  "id": "um_11111111111111111111111111111111",
  "name": "deepseek-4.1-flash",
  "enabled": true,
  "revision": 1,
  "sources": [
    {"id": "workbuddy/cn/deepseek-v4.1-flash", "enabled": true},
    {"id": "trae/cn/DeepSeek-V4.1-Flash", "enabled": true}
  ]
}
```

规则：名称保留原样且调用精确匹配，唯一性按大小写折叠检查。原始来源新绑定时由 Registry 归一为其实际大小写；规范化后去重。CodeArts 存为 `codearts/<原始ID>`，执行时转换成既有裸 ID，保留原价格/健康/策略索引。源 ID 不是可访问 URL，不能把统一名称再当来源。

所有写入拒绝未知字段、null 和超界值，除下文明确允许的 null。旧来源目录暂时缺失可保留/禁用/删除，但不能替换成不存在的新来源。编辑删除来源时若固定来源授权仍引用它，返回 409；先移除授权引用或仅禁用来源。最后一个来源不能删空，统一模型可整体停用。

### 1.3 分组授权

`AccountGroup` 新增 `revision`（正整数，初始 1）和 `unified_models: UnifiedGrant[]`（最多 128，model_id 不重复）。`AccessScope` 投影这些纯值，不能导入 config/catalog 类型。

另有两个原始调用兼容字段：`raw_model_mode: legacy|qualified_only` 和 `raw_models_visible_default: bool`。前者决定裸名称是否可以进入旧 Registry 路径，后者决定未设 visible 的原始型号是否列出。新建组默认 `qualified_only`/false；迁移有映射组用 `qualified_only`/false，无映射组用 `legacy`/true。更新省略保留，null 拒绝；管理员可显式调整，但仍受原始白名单和账号约束。不分组密钥按 legacy/true 处理。这两个字段不授予任何新来源权限。

```json
{
  "revision": 1,
  "unified_models": [
    {
      "model_id": "um_11111111111111111111111111111111",
      "visible": true,
      "callable": true,
      "source_mode": "selected",
      "source_ids": ["workbuddy/cn/deepseek-v4.1-flash"]
    }
  ]
}
```

这是分组新增字段片段，其他原有字段不变。每个 grant 的五个字段均必填；`source_mode=all` 时 `source_ids` 必须为 `[]`，表示跟随当前定义的全部来源；`selected` 时 `[]` 表示零来源，不是继承。源 ID 必须属于该定义，已保存但目录缺失的 ID 允许保留。

- 新建组缺省 `unified_models=[]`；新建 null 无效。已有组 POST 缺省该字段保留，显式 `[]` 清除所有统一授权，null 返回 400。
- 修改既有组需要 `expected_revision`；缺失返回 428，过期返回 409。旧客户端不得通过省略新字段覆盖授权。
- 旧 `model_rules` 仅管理原始 ID，继续保留 nullable visible/callable 和既有省略/null 兼容；迁移后的公开名规则进入 grant。禁用原始来源影响统一调用，原始 visible 不控制统一 visible。
- `account_ids`、原始 `allowlist_enabled/model_ids`、用户所属组和密钥绑定不变。unified grant 不能绕过原始白名单。
- grant 的完整 CodeArts source_id 在检查原始 `model_ids/model_rules` 时转换为旧裸规范 ID；不直接把 `codearts/x` 与旧白名单 `x` 比较。仅转换标识，不放宽匹配。
- 默认模型/客户端偏好继续存名称字符串。因公开名不可直接更名，引用没有隐式改名问题；保存时必须解析且可调用。客户端偏好还须可见。删除/撤销导致默认引用失效的写入返回 409，允许同次组更新先清除默认值。

组的新可选字段（unified_models、raw_model_mode、raw_models_visible_default）省略只在创建用上述默认值、在更新保留旧值；expected_revision 是写入条件，不持久化。状态文件中没有“省略等于自动授权”的表示。

### 1.4 客户端策略与能力

`ModelPolicies["unified/" + model_id]` 为可选管理员公共策略。读写继续使用 `config.ModelPolicy`，不增加另一套上下文结构。统一策略仅用于客户端建议；要求 `SummaryEnabled=false`、`SummaryRetry=false`、`SummaryFallbackModel=""`、`DisableSummaryThinking=false`，不扩大 CodeArts 专用摘要能力。

计算顺序：

1. 取该调用者授权、定义启用的配置来源集合，忽略临时健康/账号并发变化；目录缺失项仍计入“资料缺失”，不能悄悄从公共策略计算中删去。
2. 每个来源先取现有原始策略，再取有明确 Context/MaxOutput 的目录建议；任何来源没有依据时公共数值推荐不可用，独立能力信息仍可返回（`client_profile_status=missing_limits`）。
3. 无统一策略时沿用 `CommonModelPolicy` 的保守合并与 `Validate`；有统一策略时验证其上下文/输出不高于来源公共边界，其余压缩参数遵守现有约束。普通管理员填写一个很大的值不能覆盖未知来源资料。
4. 保存统一策略时验证全体启用来源；读取时再按调用者范围验证。来源/原始策略变更导致不兼容，写入先拒绝；上游元数据自然变化导致不兼容，profile 返回能力信息及 missing_limits 状态并省略未知数值，不能改投其他模型。

图片、工具能力只来自型号目录或适配器已验证的能力声明。图片复用现有三态；工具在 provider.Model 新增 `ToolCalling string`，JSON 为 `tool_calling,omitempty`，值为 supported/unsupported/空串，空串表示 unknown。由 U3/U4 添加元数据投影，不能通过模型名或一次文本成功推断。profile 的可选布尔字段 `supports_tools` 仅在公共能力明确时返回 true/false，未知时省略。新统一调用的图片/工具请求只选明确支持的来源；全部不支持或未知返回 400。原始型号的现有协议行为保留。

共享模型能力对已授权启用来源按可路由能力声明：任一来源明确支持即可声明支持；全部明确不支持才声明不支持，其余未知。组合请求仍须同一来源同时满足全部能力。健康短暂下降不能扩大声明。普通文本不要求图片/工具元数据。固定平台适配器本身的工具协议支持是必要条件，不能自动证明所有未知型号均支持。

## 2. 名称解析和权限顺序

### 2.1 名称空间

- 完整原始 ID 含斜线，直接走原始解析，不能落回统一模型或其他平台。
- 统一名称不含斜线；允许与原始短名及内置别名同名。自定义别名键或另一统一名称的大小写折叠冲突仍拒绝。公开名与完整原始 ID 分别解析。
- 管理员显式创建同名公开定义后，不分组及获授权调用者优先走该定义；完整原始 ID 保持原义。legacy 分组无该定义授权时可以继续原始裸名调用，但仍执行原始 ACL；有显式授权且 callable=false 时禁止回退。旧离线迁移仍保守拒绝名称冲突，区别于管理员新建。
- 自定义别名仅允许直接指向原始型号，不能指向统一名称/另一个别名；新增别名与统一名称冲突返回 409。旧内置跨模型别名保留在原始兼容路径，不能作为同型号来源证明。

### 2.2 推理请求

按以下顺序处理，三个协议共用模型解析，协议自身的字段合法性验证先执行：

1. 验证密钥、用户/组状态和协议必填字段；无效凭据 401，组/用户权限失败 403。已有匿名推理开关及其用户存在时的限制不变，不借全局化开启匿名调用。
2. `ForceModel` 非空仅允许在全局统一目录为空时使用，保留原始强制路径。添加任何统一定义（包括禁用）或迁移映射时与 ForceModel 共存返回冲突。
3. model 为非空时仅去首尾空白，以该名称解析；空值取组 `default_model`，再取全局 `DefaultModel`，再沿用原 Registry 空值默认。Responses 原有必填规则保留：没有组默认可填时仍 400；不能因本改造放宽协议验证。
4. 完整 ID 进入原始兼容解析；裸名称先精确查统一定义。未命中时，组 `raw_model_mode=qualified_only` 返回 404；legacy 或不分组请求才进入原始 Registry 兼容解析（原始 ID、自定义别名、内置别名及其既有大小写兼容）。仅大小写不同于现有统一名称返回 404，不转到原始兼容分支。
5. 找不到定义/原始目标返回 404。不分组请求可使用全局定义；有分组时必须有相应 grant 且 callable=true，之后求 grant 来源范围与原始白名单/规则的交集。没有配置授权来源返回 403。
6. 已授权但定义/所有来源停用、目录缺失、无账号或全在冷却，推理返回 503。先判断身份权限，不向无权调用者泄露来源健康。
7. 至少有可解析来源后验证图片、工具、原生协议等能力；无兼容候选返回 400。原子准入再复核定义、配置版本、来源权限、账号和现有预算/限额。

配置变化：计划记录 `model_routing_revision` 和统一模型 revision。原子准入先复核当前权限；仍有权限但配置 revision 不一致，返回 409 `model_configuration_changed`，不在本轮重新生成计划或扩大重试。下一次完整客户端请求使用新配置。组调整只收缩本请求已有候选并执行最新授权，不能自动加进旧计划之外的新来源。

已经取得租约并开始发往上游的尝试可以按其快照结束；新尝试必须复核。SSE 已提交输出后不重放。对已选来源记录健康与费用，不把一次来源错误计为所有来源故障。

### 2.3 列表与推荐

- `/v1/models`：只列调用者可见的统一定义；grant visible 与 callable 独立。全局禁用定义不列出；零账号、冷却、selected 空集仍可列出已授权可见定义，客户端不得据此判断实时可用。原始列表沿用旧规则。
- 原始列表先使用 `raw_models_visible_default` 作为缺省，再对被该调用者可见统一定义覆盖的来源默认隐藏；显式原始 visible=true 可显示完整来源 ID。无统一授权的旧组保持旧可见性，已映射旧组保持全部原始型号缺省隐藏。公开名每个仅一行。
- `provider/owned_by` 对统一模型使用 `unified`，`scope` 空，不冒充 CodeArts；兼容保留旧行结构及 `client_profile`。原始来源详情只在管理员接口返回。
- 显式 `/v1/client-profile?model=...` 按相同名称解析，不运行账号选号；未指定时先采用组客户端偏好，再执行原有默认逻辑。有配置但已失效的偏好返回对应错误，不悄悄换另一型号。
- profile 对已授权、已启用定义在无账号时仍可返回已有保守建议；参数不足返回能力信息及 missing_limits 状态。`/v1/models` 对该行写 `client_profile:null`，附 `client_profile_error:{code,message}`，而非使整个列表失败。
- profile `version` 仍为 1，保留既有字段，新增 `model_id`、`model_revision`、`routing_revision`；客户端可忽略新增字段。能力不支持的协议不能在 `protocols` 中宣称，原生特性标志按实际适配器计算。
- 统一 profile 的 `upstream_protocol=routed`，表示实际协议由来源适配器决定；列表与独立 profile 一致。客户端可使用的三种文本协议继续由网关转换支持，不宣称原生 WebSocket、store 或 compact 能力。
- `/user/api/models` 复用相同授权投影。管理页的可调度统计为有时间戳的快照，不能当成请求租约。

## 3. 管理 API（v1，U4 已实现）

以下新路径使用现有 `withAdminAuth`、同源写校验和审计，普通用户不得调用。新 CRUD 的 JSON 请求上限 32 KiB，拒绝尾随 JSON/未知字段；模型列表分页默认 100、上限 200，按稳定 ID 升序，`after` 为上页最后 ID，新增模型可能在下一次完整刷新才显示。既有分组端点保持 256 KiB 上限，其他原有端点沿用原 body 上限，不用新 CRUD 上限截断已有设置。

管理读写响应均 `Cache-Control: no-store`；非法 after/limit 返回 400，列表每页为单次配置快照。分页期间 routing_revision 改变时客户端重新加载，不假装多页是同一事务。

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| GET `/admin/api/unified-models?limit=100&after=...` | 无 body | 200 `{models:[...],next_after:"",routing_revision:1}` |
| GET `/admin/api/unified-models/{id}` | 无 body | 200 `{model:{...},client_policy:null,policy_key:"unified/um_...",routing_revision:1,references:[],source_status:[]}` |
| POST `/admin/api/unified-models` | `{name,enabled,sources,client_policy,expected_routing_revision}` | 201，详情结构；服务器生成随机 128-bit ID/revision=1 |
| PUT `/admin/api/unified-models/{id}` | `{name,enabled,sources,client_policy,expected_revision,expected_routing_revision}` | 200，详情结构，revision 递增 |
| DELETE `/admin/api/unified-models/{id}` | `{expected_revision,expected_routing_revision}` | 200 `{ok:true,routing_revision:...}` |

`client_policy` 在 POST/PUT 必须提供：null 删除/不设置公共覆盖；对象采用完整 ModelPolicy。来源数组为完整替换，不作部分合并。更新同样值成功不递增版本；所有版本上限达到时明确拒绝，不能回绕。目录状态、references、policy_key 为只读，写入它们返回 400。

`references` 返回组 ID、引用类别与名称，不返回密钥、用户密码或凭据；引用类别包含组授权、默认值、客户端偏好、全局默认。有外部引用时 DELETE 返回 409，成功删除同时移除该定义专属 policy；不会删除原始策略/价格/历史日志。公开名称不可变，用 PUT 改名返回 400 `immutable_model_name`。

`source_status` 每项包含 `id`、`catalog_status: present|missing`、`enabled`、`eligible_account_count`、`last_probe`（无记录为 null）、`observed_at`（UTC RFC3339）。last_probe 非空时仅有 `status: success|failed`、`checked_at`（UTC RFC3339）、`latency_ms`（非负整数）和脱敏 `message`（最多 500 字符）。复用现有探测结果；没有上述数据时返回 null，不新建伪造记录。可调度数复用现有账号/型号健康、启停和并发条件；尚未校验/续期的凭据不标为“调用验证成功”。

U4 中原探测运行时不留存满足上述字段的历史记录，因此 `last_probe` 返回 null。不能把普通请求的型号健康时间当成探测成功记录；账号数只代表准入条件快照，不证明凭据可用。

现有端点的修改：

- `GET /admin/api/groups` 增加组 revision/grants；`POST /admin/api/groups` 增加 expected_revision 和 grants，旧 model_mappings 写入返回 400 `legacy_mapping_field`；`DELETE /admin/api/groups?id=...&expected_revision=...` 要求组 revision 条件，继续原引用保护。新建组无需 expected_revision，不能通过带未知 id 的更新创建组。
- `/admin/api/groups/presets` 保留兼容读，只返回建议，不写组；应用建议的 UI 转到创建全局定义及显式授权，不自动合并来源。
- `POST /admin/api/model-policies` 保留原始型号策略写入；统一策略只从新 CRUD 写入。原始策略更新请求增加 expected_routing_revision，缺省 428；需复核所有引用统一策略并原子递增 routing revision。
- `/admin/api/settings` 修改 `model_aliases/default_model/force_model` 时要求 expected_routing_revision 并做严格提交及冲突检查。只改其他设置的既有行为不扩大到本轮。
- 原始模型刷新/探测沿用原路径、额度和显式操作，不增加自动批量付费探测。

错误继续使用现有外壳，新增字符串字段 `error.reason` 区分业务原因，不能将现有数字 `error.code` 改成字符串。示例：

```json
{
  "error": {
    "code": 422,
    "type": "invalid_request_error",
    "reason": "client_profile_missing",
    "message": "此统一模型缺少来源客户端参数，请在模型页补齐"
  }
}
```

| HTTP | reason | 条件 |
| --- | --- | --- |
| 400 | invalid_model_definition / immutable_model_name / legacy_mapping_field | 非法输入或旧写法 |
| 400 | model_capability_unavailable | 图片/工具等无兼容来源 |
| 400 | native_model_capability_unsupported | 已核实的原生型号不支持请求的图片或工具能力；不改名、丢弃内容或降级到其他型号 |
| 403 | model_permission_denied | 没有 grant、callable=false、来源授权为空或组无权限 |
| 404 | model_not_found | 未知公开名/完整原始 ID；不返回其他组目录 |
| 409 | model_name_conflict / model_in_use / revision_conflict / model_configuration_changed / model_policy_conflict | 名称、引用、并发或策略冲突 |
| 422 | client_profile_missing | profile 所需依据不足或不能合并 |
| 428 | revision_required | 更新缺少要求的版本条件 |
| 503 | model_sources_unavailable | 授权成立但无可调用来源；提示具体平台仅用于管理员详情 |
| 500 | model_save_failed | 持久化失败，旧配置继续有效 |

401、405、413、预算/限流 429、上游 4xx/5xx 及流中错误沿用协议与平台路径，不全改成上述统一模型错误。上游报错不冒充 model_permission_denied；流已开始时不能改写 HTTP 状态或重放请求。

## 4. 并发与落盘

CRUD、相关策略及模型设置更新使用现有 `saveMu -> mu` 顺序，并遵守涉及 journal 的已有更严格顺序。校验、revision 检查、引用检查与发布必须在同一次受保护提交中，保存失败回滚定义和版本。目录读取可在业务层做快照校验，不在存储锁内调用平台 HTTP。

新增 model_routing_revision 不作为权限缓存：组、用户、密钥、账号的权限仍由原子准入逐次复核。所有组修改路径（包括删除账号带来的成员更新）在实质改组时更新其 revision；额度/用量变化不影响编辑 revision。

`client_policy` 的编辑属于模型修改，同时递增模型 revision 和 routing revision；原始策略修改只递增 routing revision。上游目录刷新使用请求开始时借用的现有目录快照，不改变存储 routing revision；不把新刷出的候选插入已在执行的计划，下一次请求读取新元数据。

`gateway.AdmissionRequest` 传递统一模型 ID/revision、routing revision、公开名和本次候选集合；接口纯值化，不让 gateway 导入 catalog/storage。`storage` 在当前全局定义和原始 ACL 上做交集后调用 scheduler。只改接口/实现消费方，不增加第二份 Registry 或权限锁。

## 5. 计量和历史兼容

`RequestedModel` 记录客户端输入，`PublicModel` 记录统一名称，`Model`/`AttemptDetails[].Model` 保留实际原始目录型号。依旧由每次尝试的原始价格算成本、预留/结算预算。未知价格保持未知和既有预算保护，不能当零费用。对外响应模型字段的改名只发生在协议外壳，不能修改工具参数中的 model 字符串。

旧 schema 10 的 Prices、ModelPolicies 原始键、ModelHealth 原始键和账本不批量改名。已有摘要备用规则保留于原始路径；统一策略不能启用专用摘要保护或跨模型备用。U3 兼容细化：保留原始来源上已明确配置的摘要完整性保护，避免旧请求暴露不完整正文；任何不在统一来源集合中的摘要备用仍拒绝，即使原始白名单允许。不能利用原始策略间接超出“同型号切换”范围。

## 6. U1 固定的实现顺序

U2 先实现类型/原子存储/迁移核心；U3 接入唯一解析和原子选号；U4 才将 API 草案合入现有 `api/admin.openapi.yaml` 并生成 TypeScript；U5 接入页面；U6 验证完整系统和升级。U1 不向现有 OpenAPI 注册尚不存在的路由，不提前创建业务空包。

## 2026-11-06 默认目录补充

`GET /admin/api/model-defaults` 返回版本化对照表与当前 Registry 投影，包含 `version/catalog_revision/routing_revision/providers/models`；每项包含公开名、来源、create/update/unchanged/conflict 状态。静态表只匹配明确平台和精确型号（大小写归一由 Registry 执行）；其他型号使用平台前缀，不能按显示名相似度合并。预览不执行网络请求。

`POST /admin/api/model-defaults` 接收版本、目录摘要、配置 revision、所选名称，以及可选 group_id/expected_group_revision，最多 512 项。服务端重建预览，只允许选取本次有效项目。storage 在原 saveMu/mu 下复核并一次保存；批次保留已有策略/禁用/来源，分组只为尚未授权模型追加账号与原始 ACL 内的固定来源，已有授权完全保留。重复应用无变化不递增 revision；失败回滚整批。

默认目录不自动修改每次请求的默认型号、不自动增加固定授权、不发起真实推理。状态字段布局仍为 11；旧二进制可能拒绝新合法名称，不支持退回旧版本。历史验收中的严格名字限制是当时版本的结果。

### 默认目录版本 2

完整厂商型号 ID（含版本及变体）在平台间大小写归一后共用公开名称。规则显式限定厂商型号格式；不使用显示名模糊匹配，不把 opaque/auto/内部代号猜成厂商型号。不新增来源能力声明。

兼容识别只匹配版本 1 自动生成的 provider-prefixed 名称、没有独立客户端策略、且标准定义覆盖每项同 ID/同启用状态来源的旧定义。旧定义和调用权限保持；仅当调用者能看见标准定义且来源授权覆盖旧范围、可调用性不降低时，列表隐藏该重复入口。管理列表另返回 compatibility_aliases 映射，管理页默认折叠并提供显式查看。新建组默认选标准模型，已授权的旧条目仍可编辑。

### Pi 图片配置同步修复

`/v1/client-profile` 成功响应分为 ready（含已验证数值）和 missing_limits（只含身份、能力、配置 revision 和错误说明）。认证、权限、未知名称及停用错误保持原状态码。旧启动器需要数值时明确提示缺项；Pi --sync-models 通过列表独立更新图片能力，同时保留并报告已有用户数值，不为新型号伪造限制。

图片原样保留在入站请求中。已确认支持视觉和工具的来源可承接两者组合；分别由不同来源提供的能力不能拼接成组合支持。真实多模态验收须包含 Pi 发出的图片块、上游识别结果和工具图片回填。
