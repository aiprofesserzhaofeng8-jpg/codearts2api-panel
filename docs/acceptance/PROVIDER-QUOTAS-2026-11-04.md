# 四个平台额度查询验收（2026-11-04）

## 问题与结果

v0.3.9 的 Qoder、Kiro、ChatGPT、Claude 适配器只有 Credit 占位结果，账号池因此显示「额度未返回」。本批接入各平台只读统计接口，并将 Credits、订阅时间窗口与本地 USD 预算分开显示；不会把窗口利用率换算成积分。

- Qoder CN/global：套餐、附加、可用组织共享 Credits；保留小数，cap=-1 仅显示未设上限；拒绝将超限全零退化响应当成确认余额。
- Kiro OAuth/API Key：按账号地区查询套餐、有效试用和赠送 Credits；精度字段的显式 0 优先于整数回退字段，分别展示重置与到期时间。
- ChatGPT/Codex OAuth：读取实际 primary/secondary 时长，包含附加窗口；保留额外 Credits 的精确十进制字符串，不假定余额为 USD。
- Claude OAuth：按上游确实返回的短窗、周窗和细分周窗口展示；缺失/null 不补成 0%。
- OpenAI/Anthropic API Key：明确显示不支持订阅窗口查询。此接入类型需要在官方 API 控制台查看实际账单与用量。

数值未知、真实 0、无上限、查询失败分别表达。超过 100% 的用量保留原值；浏览器只限制进度条视觉范围。重置时刻到达后提示刷新，不自行清零。

## 自动验证

环境：Linux amd64，Go 1.27.3，低并发 `GOMAXPROCS=2 GOFLAGS=-p=2`，隔离临时状态及本机模拟上游。

1. `make check GO=…`：前端类型/构建、45 项前端测试、生成接口一致性、Go vet/全包测试、HTML/CSP/架构检查及部署/Python 回归通过。
2. `go test -race ./internal/accounts ./internal/chatgpt ./internal/claude ./internal/kiro ./internal/qoder ./internal/server -run 'Test(Quota|ProviderQuota|Credit)' -count=1`：通过。最后修订后重跑相关测试。
3. `python3 scripts/frontend_e2e.py --binary artifacts/quota-panel-test --output artifacts/quota-browser`：20 项浏览器场景通过，包括新增中英文额度场景、390px 移动端、强制刷新失败与恢复、旧积分展示、精确余额和真实 0。
4. 新增服务集成测试覆盖四平台管理员查询、普通 API Key 拒绝访问、60 秒缓存和 fresh=1 回源。模拟上游 401/403/429 时错误体脱敏，不改变账号启用/健康状态，不写推理日志或修改持久化状态。
5. 截图人工复核中英文桌面与移动端；文档本地链接和 `git diff --check` 通过。

查询沿用原有单账号 15 秒期限与四路并发。凭据近到期时仍委托既有凭据协调器与条件提交，不增加第二套刷新路径。DTO 字段为兼容性扩展，状态 schema 保持 10。

## 未覆盖范围

本次会话不能免密码读取 root 管理的 `/opt/codearts2api/data/state.json`，未读取、导出或使用其中的真实账号。本报告不声称真实 OAuth 权限、地区网络、第三方协议兼容性、真实余额或自然重置已验证。

源码、测试程序与已安装系统服务分别管理。此次本地验证没有重启、卸载或替换已安装服务。需更新程序并在账号池点击「刷新额度」后，才能核实真实返回；若查询失败，应查看该账号的可见错误信息，不将错误猜测成零余额或整个账号失效。

协议依据与数据语义见 [账号额度与用量](../ACCOUNT-QUOTAS.md)。
