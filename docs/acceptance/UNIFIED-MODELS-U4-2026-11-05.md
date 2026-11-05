# 全局统一模型 U4 验收

日期：2026-11-05。范围：[实施计划 U4](../architecture/UNIFIED-MODELS-PLAN.md#u4列表推荐参数与-http-契约)。结论：U4 的列表、推荐参数与 HTTP 契约已实现并通过隔离检查。U5、U6 未完成，系统服务未升级，未推送或发布。

## 实现结果

- `/v1/models` 和 `/user/api/models` 使用全局定义与分组 grant 投影。公开名称只出现一次，provider/owned_by 为 unified；可见定义不因零账号或冷却从列表消失。覆盖来源默认隐藏，显式原始 visible 仍可显示完整来源 ID，callable 与 visible 分开处理。
- `/v1/client-profile` 与推理共用精确名称解析。未指定型号时采用显式客户端偏好，再沿用推理默认；失效偏好报告原错误。没有账号时仍能返回有依据的公共参数；来源缺资料返回 422，列表对应行为 client_profile:null 和独立诊断。
- 推荐参数包含 model_id/model_revision/routing_revision，version 仍为 1。公共策略验证所有启用且授权的配置来源；目录缺失但已有原始策略可以继续计算，没有依据不能忽略该来源。图片/工具声明保持保守，缺失目录不能扩大能力。
- 列表整次复用一份配置快照，逐行读取不再复制全局定义；统一推荐的原始策略也来自该快照，不混用另一个配置版本。没有新增网络调用、权限缓存、租约或重试层。
- 新增统一模型列表、详情、创建、修改、删除五种管理员操作。32 KiB 严格对象、分页、不可变名称、revision、无变化保存、来源校验、引用保护和失败回滚沿用既有事务。
- 分组 GET 返回规范 grant；POST/DELETE 要求既有组版本，拒绝 model_mappings 写法，省略新字段保留已有授权。原始策略写入、全局模型设置写入要求 routing revision。其他设置仍保留既有保存语义。
- 新路由复用管理员服务器会话、同源校验及审计。管理响应和 profile 不缓存；不向普通用户返回来源详情、账号或凭据。
- 错误保留数字 code，新增 reason。已验证 400/403/404/409/422/428/503/500 的原因区分；平台执行错误继续沿用原路径，未冒充网关权限拒绝。
- OpenAPI 与生成的 TypeScript 已同步，新增模型管理、公开列表/profile 结构及组写入条件；没有提前加入 U5 编辑页面。

## 主要文件

| 职责 | 文件 |
| --- | --- |
| 全局列表、保守策略和共享解析 | [unified_projection.go](../../backend/internal/catalog/unified_projection.go)、[unified_routing.go](../../backend/internal/catalog/unified_routing.go) |
| 管理详情与来源快照 | [unified_detail.go](../../backend/internal/catalog/unified_detail.go)、[unified_status.go](../../backend/internal/storage/unified_status.go) |
| HTTP CRUD / 严格解码 | [unified_models.go](../../backend/internal/server/unified_models.go) |
| 分组和配置协调 | [unified_management.go](../../backend/internal/access/unified_management.go)、[management.go](../../backend/internal/config/management.go)、[model_management.go](../../backend/internal/config/model_management.go) |
| 列表与 profile HTTP | [models.go](../../backend/internal/server/models.go)、[client_profile.go](../../backend/internal/server/client_profile.go) |
| 接口契约 | [OpenAPI](../../api/admin.openapi.yaml)、[生成类型](../../frontend/src/api/generated/admin.ts) |

## 验收覆盖

新增 11 个 HTTP 顶层测试和 1 个存储快照测试，见 [unified_api_test.go](../../backend/internal/server/unified_api_test.go) 与 [unified_status_test.go](../../backend/internal/storage/unified_status_test.go)。测试通过真实 Handler 鉴权、输入、业务事务及输出路径，使用临时状态、虚构凭据与本地假上游。

| 用例 | 已验证结果 |
| --- | --- |
| 同一密钥、名称的列表/profile/三协议 | 六类成功响应（3 协议 × 非流式/SSE）保留公开名；无账号的另六类请求返回 503 |
| 无分组、固定授权来源 | 全局名称可见；selected 仅用指定来源计算参数；原始可见性不能修改统一授权 |
| 缺元数据、大小写与失效偏好 | 列表继续 200，单行诊断；独立 profile 为 422/404/503，未偷换型号 |
| CRUD、分页和并发条件 | 稳定 ID 分页、无变化不递增、改名 400、旧版本 409、缺版本 428、引用删除 409 |
| 请求校验 | 未知/只读字段、null、尾随 JSON、缺字段被拒；超出 32 KiB 返回 413 |
| 角色和同源保护 | 推理密钥 401、普通用户所有管理方法 403、跨站管理员写入 403、写操作记录审计 |
| 三协议错误来源 | 未知名称和撤销授权均带网关 reason；WorkBuddy 上游 403 仍按既有凭据错误映射为 502，不冒充 model_permission_denied |
| 公共策略和原始策略 | 超出边界拒绝且配置不变；原始策略更新可借用其他来源目录资料，公共策略 null 清除 |
| 持久化失败 | 返回 500 model_save_failed，模型及组合设置/版本均回滚 |
| 来源可调度数 | 启停、冷却、并发饱和和释放如实反映；只读快照不占位、不验证凭据 |

既有 U3 的跨平台、粘性、取消、撤权、价格、摘要边界与输出后不重放继续回归。检查详情和日志摘要见 [机器可读记录](UNIFIED-MODELS-U4-CHECKS-2026-11-05.json)。

## 验证方式

Go 1.27.3；所有 Go 检查设置 GOMAXPROCS=2 GOFLAGS=-p=2，直接 Go 命令前已构建前端。运行：make check；专项 HTTP/路由测试；storage/catalog/server/config/access/apierr 的相关 race；Windows amd64 vet；独立二进制构建。静态检查包括生成契约、架构依赖、格式及本地文档链接。

## 边界

旧组映射编辑器尚未切换，直接对 U4 后端提交 model_mappings 会被拒绝；模型设置/原始策略旧页面也尚未携带新的版本条件。这些页面在 U5 改造，本阶段不应作为完整前端升级包投产。

原探测运行时没有保存满足完整结构的历史探测结果，因此详情 last_probe:null；可调度账号数仅为带时间戳的本地条件快照，不是调用成功证明。没有为了填展示字段自动发起付费探测。

本阶段未操作 /opt 下运行数据、生产账号、系统服务或用户 Pi/Codex 配置。真实双来源验证、Pi 端到端和安装升级恢复仍归 U6。
