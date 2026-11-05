# 全局统一模型 U1 验收记录

日期：2026-11-05。范围：U1 契约冻结与基线。状态：U1 已完成；新功能仍待 U2–U6 实施和运行验收。

## 1. 交付清单

| U1 要求 | 交付物 | 内容 |
| --- | --- | --- |
| 架构取舍和模块归属 | [ADR 0018](../adr/0018-global-unified-models.md) | 唯一参考 LiteLLM；全局定义、组授权、原子准入、版本与兼容边界 |
| 字段与 API 草案 | [契约 v1](../architecture/UNIFIED-MODELS-CONTRACT.md) | JSON 字段、缺省/null/空集、5 个新 CRUD 方法、旧接口修改、版本冲突、错误码 |
| 迁移清单与权限真值表 | [迁移与验收矩阵](../architecture/UNIFIED-MODELS-MIGRATION.md) | 确定性转换、24 个权限案例、24 个迁移案例、T01–T16 验收位置 |
| 代码和依赖基线 | [基线 JSON](UNIFIED-MODELS-U1-BASELINE-2026-11-05.json) | HEAD、27 个文件摘要、20 个 Go 文件的直接内部导入扫描、工具链声明与现有差异 |
| 后续执行入口 | [实施计划](../architecture/UNIFIED-MODELS-PLAN.md) | U1 完成条件、U2–U6 依赖和限制 |

## 2. 源码关系核对

| 主题 | 已核实源码 | 结论与实施约束 |
| --- | --- | --- |
| 原始目录与别名 | [registry/registry.go](../../backend/internal/registry/registry.go)、[provider/provider.go](../../backend/internal/provider/provider.go) | CodeArts 裸 ID 与其他平台完整 ID 并存；内置别名会跨模型，不作为同型号证明；完整来源需转换到原始内部键 |
| 定义与授权 | [access/models.go](../../backend/internal/access/models.go)、[access/management.go](../../backend/internal/access/management.go) | 旧 ModelMappings 归组；省略/null 规则需在新字段明确，不能误将空集合解释为全部 |
| 列表和默认值 | [catalog/routing.go](../../backend/internal/catalog/routing.go)、[server/responses.go](../../backend/internal/server/responses.go) | 已映射组裸名称限制、原始默认隐藏、客户端偏好与推理默认不同；Responses 原有必填验证需要保留 |
| 原子准入 | [storage/pool.go](../../backend/internal/storage/pool.go)、[gateway/admission.go](../../backend/internal/gateway/admission.go) | 权限/实际型号/选号/占位仍在 Store 原锁内，scheduler 仅算法 |
| 策略与计价 | [config/model_recommendations.go](../../backend/internal/config/model_recommendations.go)、[storage/pricing.go](../../backend/internal/storage/pricing.go) | 公共策略缺失来源时停止；价格按实际尝试型号，不改为公开名称统一计价 |
| 日志 | [usage/types.go](../../backend/internal/usage/types.go) | 已有 RequestedModel、PublicModel、AttemptDetails.Model，优先复用 |
| 迁移与升级 | [storage/compatibility.go](../../backend/internal/storage/compatibility.go)、[deploy/panelctl.py](../../deploy/panelctl.py) | Open 有迁移/重放/Save 副作用，预检查不能直接调用；现有升级仅支持相同 schema，需受限跨 schema 流程 |
| 目录查询网络行为 | [server/models.go](../../backend/internal/server/models.go) | 已有 stale 刷新会查询目录；本轮只承诺不新增推理探测，不把“列表请求完全离线”当现状 |
| 依赖约束 | [tools/check_architecture.py](../../tools/check_architecture.py) | access/scheduler 不能导入 config/catalog，catalog 不导入 storage/server/平台 HTTP |

## 3. 本轮明确解决的歧义

1. 稳定 ID 与公开名称不同；第一版不可直接改名，通过显式新建和迁移引用实现。
2. 同名同来源才合并；名称和原始/别名冲突即停止；未来目录新增同名裸型号不能抢占现有公开名称。
3. 组授权缺失与空列表拒绝；all 必须显式选择。旧有映射组的裸名限制/隐藏默认保存为两个兼容字段。
4. 客户端偏好只用于推荐，ForceModel 和统一目录互斥。不同协议原有必填校验不被路由默认偷偷放宽。
5. 新定义、策略及模型相关设置使用 CAS；组有独立编辑 revision；过期请求计划返回 409，不新增重试层。
6. 没有账号是推理 503；策略缺失是 profile 422；profile 200 不保证推理成功，列表不发付费探测。
7. 错误数字 code 保持，新增 reason 标识；上游 403 不冒充网关分组拒绝。
8. schema 11 使用新授权结构，旧程序必须拒绝。跨 schema 切换分别处理预检查、停写再检查、备份、副本迁移和失败恢复。
9. 统一策略不启用 CodeArts 专用摘要备用，价格和历史结算仍使用原始键。

## 4. 基线与操作边界

- 仓库：`/home/white/codearts2api-panel`，分支 `main`，HEAD `6e8a6cdfa182cf697c915fb05a41e46ae6900cee`。
- schema 10；Go 声明 1.27.0、toolchain go1.27.3；frontend 声明 pnpm 10.34.9、Node >=24.11.0 <25；本次观测 Node v24.11.0、Python 3.12.7。当前 go.mod 无 require 块，未新增依赖。
- 开始时已有 `scripts/connect-client.py` 未提交修改（模型列表与 422 提示），本轮未修改；其基线 SHA-256 为 `94ca9a14900792421df6a12f6226b8e8e3a082c0b8dd5370fb3c18c170c11100`。
- 开始时计划未跟踪、架构索引已修改；本轮以这些内容继续编写，没有回退工作树。
- 17:23 CST 的只读 systemd 查询显示 `codearts2api.service` active、PID 2838、启动时间 14:37:38 CST。尝试读取安装二进制版本被文件权限拒绝，故不推定运行版本等于仓库 HEAD。
- 未读取生产 state、API key 或原始会话内容，未真实调用模型、迁移数据、重启服务、提交或发布。U1 不改变 Go/Python/前端业务行为。

## 5. 检查方法与结果

最终检查范围：所有 U1 文档相对链接、JSON 示例解析、24 个 P 编号和 24 个 M 编号完整性、原计划 16 个 T 编号覆盖、基线文件摘要一致性、diff 空白检查。只证明文档/规格完整性，不把规格案例计为已通过的实现测试。

检查结果：本地链接、JSON 示例、编号覆盖和 27 个基线文件摘要均通过；`git diff --check` 通过。P01–P24、M01–M24 和 T01–T16 均唯一且连续。基线和检查结果同时记录在 JSON 证据中。检查使用 Node 标准库解析 Markdown 链接、JSON 示例、用例编号和 SHA-256，没有执行生产迁移。

本轮不运行 make check/race 或真实上游测试，因为业务代码没有修改；已明确未来每项测试的阶段和落点。完整生产依赖图的编译检查留给 U2 代码变更，基线 JSON 的 import 扫描仅为静态参考。

## 6. U1 完成判据

- ADR、字段/API 草案、迁移清单、权限真值表、基线齐全。
- 名称顺序、空值、来源授权、版本冲突、错误和升级失败均有明确结果。
- 本轮仅修改文档；既有启动器差异和所有基线业务文件保持。
- 文档检查通过，计划 U1 标为完成；U2–U6 未开始，下一项为 U2。
