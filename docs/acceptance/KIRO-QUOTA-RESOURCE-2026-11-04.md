# Kiro Credits 资源识别修复（2026-11-04）

v0.3.10 在 `unit` 不等于 Credit/Credits 时提前放弃统计，遗漏了 Kiro 的 `resourceType=CREDIT, unit=INVOCATIONS` 响应。管理员页面因此仍显示「上游未返回 Kiro Credits 数量」。这是解析缺陷，不足以据此断言账号余额为空或无权限。

## 复现与修复

公开的原始响应来自 [CodexBar issue 3359](https://github.com/steipete/CodexBar/issues/3359)。测试保留语义类型、底层单位、展示名称、整数/精度数值与重置字段，不使用其中的个人身份或真实凭据。

新增 `TestQuotaCreditResourceUsesInvocationUnit` 在修复前失败，产出与截图一致的 unknown 原因。修复后先识别语义资源，兼容 INVOCATIONS/COUNT 等底层计数单位；明确的 CREDIT 优先于旧 AGENTIC_REQUEST 或缺省记录。其它资源、含义不明的调用计数和矛盾的 Token 单位不展示成积分。缺少计数仍为未知，明确的 0 与精度字段优先级保持。

额外覆盖 Token 与 Credits 混合、列表顺序变化、明确资源优先、有效试用、精度 0、缺失数值，以及两地区和 OAuth/API Key 的实际 HTTP 路径。管理员端到端测试也改用含 CREDIT/INVOCATIONS 的响应，验证经过原有缓存、权限和 JSON 投影后可返回额度。

## 验证与边界

- 修复前的真实形状回归：FAIL，确认能够复现旧行为。
- 修复后的 Kiro 与 server 额度测试（含 race）：通过。
- `make check`：前端 45 项测试、Go 全包测试和 vet、生成接口、静态资源/架构及部署工具检查通过；最后补充的资源排除条件另行重跑 Kiro/server 的额度 race 测试通过。
- 发布验证与远程 CI 由 GitHub Actions 记录。
- 单实例当前 root 服务和账号数据未修改；本次仍无法免密码读取安装实例数据。未声称已用该用户的真实账号回源验证。

需要更新程序后点击「刷新额度」。如果上游返回其它未覆盖形状，仍应保留未知而非补成 0。
