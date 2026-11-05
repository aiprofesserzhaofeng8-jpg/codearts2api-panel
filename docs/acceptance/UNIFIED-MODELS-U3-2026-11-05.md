# 全局统一模型 U3 验收记录

日期：2026-11-05。范围：全局名称解析、原子准入和同型号来源切换。状态：U3 已完成，实现及隔离验收通过。未替换本机服务或迁移生产数据。

## 实现

| 交付 | 文件 | 行为 |
| --- | --- | --- |
| 全局解析 | [catalog/unified_routing.go](../../backend/internal/catalog/unified_routing.go) | 不分组和有分组请求统一读取全局定义；公开名精确匹配，完整原始 ID 兼容解析 |
| 来源授权 | [access/unified_models.go](../../backend/internal/access/unified_models.go) | 显式 grant、selected/all、空授权拒绝；原始白名单和 callable 继续限制来源 |
| 请求计划版本 | [catalog/routing.go](../../backend/internal/catalog/routing.go) | 本请求计划携带统一 ID、模型 revision、routing revision；过滤不修改共享目录 |
| 原子准入 | [storage/unified_admission.go](../../backend/internal/storage/unified_admission.go) 与 [pool.go](../../backend/internal/storage/pool.go) | 在现有 Store 锁内复核最新定义、授权及候选，再选号/占位；新授权不能加入旧请求候选 |
| 执行协调 | [gateway/admission.go](../../backend/internal/gateway/admission.go) | 传纯值版本与来源集合，复用原调度和预算协调，不导入 catalog/storage |
| 错误与切换 | [server/account_retry.go](../../backend/internal/server/account_retry.go)、[apierr.go](../../backend/internal/apierr/apierr.go) | 403/409/503 保持身份权限/配置变化/无来源区别；瞬断在原总尝试范围换号，不损伤账号健康 |
| 能力筛选 | [provider.go](../../backend/internal/provider/provider.go)、[unified_routing.go](../../backend/internal/catalog/unified_routing.go) | 三态工具能力字段，结合图片声明及已知输出上限筛选来源，未知不宣称支持 |

三种 HTTP 推理入口在协议转换后使用同一能力筛选。只检查消息内容类型、工具声明和输出上限，不递归改写工具 schema/参数，不丢弃图片。消息历史中存在工具调用/结果也要求工具能力。原生 Messages 的 thinking/签名仍沿用现有平台限制。

工具能力元数据只在有可信声明时设置；本阶段没有把所有平台/型号自动标成 supported。未知型号的统一名称工具请求会明确拒绝，后续可依据平台确切元数据补齐，不能用一次文本成功替代能力验证。

## 权限与配置变更

- 不分组密钥可以通过全局名称创建计划，仍受实际账号启停、额度、并发和健康限制。
- 组必须有对应 grant，原始型号白名单和来源 callable 在解析与准入两处检查；当前账号范围在 Store 选号时复核。
- 撤销 grant/来源授权返回 403；授权成立但模型定义或策略版本改变返回 409，不另开重解析循环。
- 只有当前定义中启用、当前授权允许、且原请求计划已包含的来源可以选号。管理员新增授权来源不能扩张已在执行的请求。
- 已取得租约的尝试不因目录刷新重写请求；下一次尝试再复核最新权限和配置。取消及时释放，输出提交后不重放。

## 摘要兼容处理

核对发现旧映射调用已使用原始 CodeArts 策略中的摘要完整性保护。直接取消会让旧摘要场景暴露不完整正文，因此本阶段保留已明确配置的原始完整性保护；不自动新增该能力，不让统一策略启用它。

原始策略的备用型号若不在统一来源授权中，即使原始白名单允许，也返回 403。这样保持旧完整性保护，同时满足“统一名称不能跨到另一型号”。原始完整 ID 的摘要备用与权限回归继续运行。相关测试由旧跨型号期望调整为“全局统一调用不能逃出来源”，不是删除摘要完整性验证。

## 验收覆盖

新增 14 个 U3 顶层测试，包含子场景：

- storage 3 项：不分组原子选号与忙碌切换、权限优先于版本冲突、扩权后旧请求不增加候选，均检查租约释放。
- catalog 3 项：全局名称/大小写/裸名限制、图片工具与输出限制筛选、无分组版本变化复核；请求数据和共享计划不被修改。
- server 8 项：不分组两平台 × 三协议 × 流式/非流式；503 诊断及未知工具拒绝；旧计划第二次尝试前停止；取消释放；网络瞬断换号且不损伤账号；业务错误状态；三协议图片及大整数工具 schema 保真；502 有界重试及上游 403 停止。

既有 mapped 测试继续验证同名平台切换、粘性会话、平台请求体独立、原始价格计量、并发、预算以及输出后不重放。原始摘要测试继续覆盖三协议的来源/账号撤权、目录变化、冷却和预算限制。

所有集成使用内存或本地假上游及虚构凭据。没有使用生产密钥、消耗真实上游额度或改写用户 Pi/Codex 配置。

## 验证命令

直接 Go 前已执行 `make frontend`。固定 Go 1.27.3，所有构建/测试使用 `GOMAXPROCS=2 GOFLAGS=-p=2`。

```text
make check
go test ./internal/storage ./internal/catalog
go test ./internal/server -run 'TestUnified|TestMapped|TestRawSummary'
go test -race ./internal/storage ./internal/catalog ./internal/gateway ./internal/server \
  -run 'TestUnified|TestMapped|TestRawSummary|TestPoolCapacityAndCancellation'
GOOS=windows go vet ./...
go build -o /tmp/codearts-u3-panel ./cmd/panel
go test -race ./internal/gateway ./internal/apierr
```

结果与日志摘要见 [U3 检查记录](UNIFIED-MODELS-U3-CHECKS-2026-11-05.json)。完整 make check 后增加的 HTTP 能力/重试用例另行定向验证并纳入 race，不把前一轮日志当作新增用例的结果。

## 阶段边界

U3 生产推理路径读取全局定义并传版本化计划；U2 的旧映射投影仍服务于尚未切换的管理页面、模型列表/profile 和测试夹具。列表、客户端参数及新管理 HTTP 契约是 U4，页面是 U5，不能宣称本阶段已完成这些功能。

本阶段检查能力声明和明确输出上限，没有实现新 tokenizer 或未知上下文容量推断。公共 profile 和各来源上下文建议的完整一致性留 U4。

构建产物 `/tmp/codearts-u3-panel` 仅供隔离验收，没有升级现有系统服务。下一项是 U4：列表、推荐参数和 HTTP 契约一致性。
