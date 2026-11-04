# Kiro 原生平台接入验收（2026-11-03）

## 范围和结果

本次在 `/home/white/codearts2api-panel` 添加独立 Kiro 适配器，沿用既有账号、目录、准入、重试、分组映射、提示词和计量架构。原有 Qoder 未提交修改保留。没有修改 `/opt/codearts2api`、真实 `data/state.json` 或远程服务，没有发布 GitHub Release。

本地自动验证通过；真实 Kiro 上游未验收，不能据此宣称任意订阅、地区或 API Key 均可用。使用说明见 [KIRO.md](../KIRO.md)，协议端点和固定参考提交见 [UPSTREAM.md](../../backend/internal/kiro/UPSTREAM.md)。

## 实现

- Builder ID OIDC 设备授权，按上游间隔轮询；检查仅预览，显式确认保存。取消、过期及旧授权重放不恢复已删除账号。
- 导入 Kiro API Key 或完整 OAuth JSON。API Key 不触发 OAuth 刷新，也不伪造过期日期；OAuth 按原发令牌地区刷新，轮转后先由原协调器条件提交。
- 美国、欧洲地区独立路由。API Key 的目录与生成使用 Q 服务根路径及不同 target；OAuth 保留 AI_EDITOR 原生端点。没有失败后跨地区或跨端点试探生成。
- 目录动态发现、分页有界；原始型号保留 `kiro/<region>/<modelId>`。统一公开名称与原始型号白名单沿用现有权限路径，不预置猜测型号。
- `/v1/chat/completions`、`/v1/responses`、`/v1/messages`，支持流式及非流式；保留工具、合法拒绝、上下文和大整数。AWS EventStream 校验双 CRC，坏帧或未完成工具不报告成功，部分输出不换号重放。
- 账号和用量筛选、地区选择、长期密钥显示、公共提示词继承及 Kiro 覆盖。管理员接口继续要求管理员会话。
- Linux / Windows 打包和 Docker 镜像包含协议参考许可。

## 已执行验证

使用 Go 1.27.3、`GOMAXPROCS=2 GOFLAGS=-p=2`，测试数据为临时状态和假凭据。

1. 根目录 `make check GO=/home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go`：通过。包含前端类型/构建/37 项单元测试、API 类型同步、Go vet、全部 Go 测试、架构检查和 86 项部署测试等。日志：`artifacts/kiro-check.log`。
2. `backend/` 执行 `go test -race ./internal/kiro ./internal/accounts ./internal/server -run 'Kiro|TestCredentialNormalization|TestRefreshUsesIssuer|TestDeviceFlow|TestBinaryStream|TestRequestMapping|TestCatalogPagination|TestHTTPRedirects|TestProductionRoutes|ProviderCredentials|ProviderLogin' -count=1`：三个包均通过。日志：`artifacts/kiro-race.log`。
3. 隔离二进制的 `scripts/frontend_e2e.py`：18 项场景均通过，包含新增 `kiro-smoke` 及原有平台、用户、分组和语言页面。结果：`artifacts/kiro-e2e/browser-results.json`。页面测试在最后一次仅后端路由调整前执行，前端资源未再变更。
4. 最新后端已构建为 `artifacts/kiro-panel-test`，未替换正在运行的服务。`git diff --check` 和新增文档本地链接检查通过。

Kiro 服务集成测试覆盖两地区、API Key / OAuth、三协议及流/非流共 24 种文本请求，以及 6 种工具输出组合；另覆盖真实生产 URL 的离线捕获、目录分页、预览保存、管理员边界、长期密钥持久化、提示词保存、Kiro / WorkBuddy 统一映射、分组拒绝、损坏流的计量与租约释放。

## 未覆盖与限制

- 尚未用真实账号完成设备登录、模型目录、API Key 生成请求或自然到期续期。
- 未实现 Google/GitHub 浏览器授权、企业自定义 Start URL、配额、签到和独立搜索；完整凭据导入与设备授权是不同入口。
- 原生协议没有独立 system 权限字段，角色文本以明确标记前置；不保证与原生 system 权限完全等价。
- 不支持强制工具、禁止并行工具、显式思考控制、结构化输出和采样控制。`max_tokens` 为兼容客户端而接受，但原生输出预算由 Kiro 控制，不能保证执行。
- 仅接受受限大小的 base64 图片，不读取远程图片。计量未知时不把 credits 伪造为 token 或 USD；严格 USD 预算仍按既有模型价格与未知用量规则处理。
- 未部署、重启系统服务、提交 Git 或同步 GitHub。测试产物留在忽略目录，真实凭据未加入测试或文档。
