# 安装与更新交付验收

依据 [实施计划](../architecture/DEPLOYMENT-DELIVERY.md)。基线为 `64f4191`；最终交付版本为 [v0.3.3](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/tag/v0.3.3)，功能、正式发布与本机升级验收已完成。初次迁移使用 v0.3.1 候选，随后通过公开安装入口升级。

## 已实现

- `deploy/install.sh`：检查稳定版本、指定稳定版本、校验后下载/安装/升级；安装和升级委托同一 Release 的 `panelctl.py`。固定 API/附件来源，重定向白名单，摘要和大小限制，缺附件或校验失败不会执行运维操作。支持服务端 `GITHUB_TOKEN`，只发送固定 GitHub API。
- `/admin/api/version`：管理员专用只读接口，当前版本来自二进制启动参数。显式查询、8 秒 HTTP 超时、2 MiB 响应限制、一分钟缓存、并发请求快速返回。开发版本、旧版本、缺附件、限流、无发布、错误分别处理。
- 配置页“版本与更新”：实际版本、检查按钮、状态和发布说明；不保存配置、不执行升级。已有统一 API 生命周期在退出时取消读取。
- `panelctl.py migrate`：验证旧 unit、实际 PID、可执行文件、数据路径和受支持参数；校验新包后停止旧服务，完整备份与恢复至新目录；保留禁止刷新选择。候选启动后失败不会重启旧数据副本。
- Release 工作流增加独立 `install.sh` 附件和摘要，以及真实用户 systemd 迁移/升级故障验收。

## 验证

- 部署单元测试：46 项通过，包含下载完整性、缺失/重复附件、非法来源、清单路径及迁移失败阶段行为。
- 前端：6 个测试文件、21 项测试通过；新增版本卡片成功/退出竞态测试。
- Go 更新检查与管理员授权测试、相关 race 测试通过；未登录 401、普通用户 403，管理员可读取实际版本。
- 实际 Chrome：隔离初始化与登录后显示 v0.3.1，点击检查不标记配置脏状态、无页面异常。第一次匿名 GitHub 查询实际遇到 403 限流；补齐提示后，使用既有 GitHub 认证进行隔离查询，确认当前公开 v0.1.0 缺附件。未保存或输出认证值。
- systemd 安装/升级：9 组验收通过，包括摘要错误/降级不重启、未就绪候选停止并保留新数据与账本。
- systemd 源码迁移：8 项通过，原管理员密码、API 密钥、私有子目录保留，旧服务停止并禁用，非空账本请求和费用只结算一次。
- 本地 CA TLS：D2 11 组场景通过；基础隔离进程/Caddy 8 项通过；未访问真实模型或领奖接口。
- amd64 实际运行；arm64 交叉编译，未进行 ARM 真机运行。actionlint 和差异空白检查通过。

完整应用回归曾发现既有 `TestGrowthClaimConcurrentIsolationAndConfirmation` 偶发在六次同时登录时触发密码验证并发限制。测试改为预先登录、并发复用会话；业务并发语义不变，生产认证逻辑未改。该测试 race 重复 10 次通过。

## 交付边界

- 候选包：`artifacts/delivery-validated/`，含 amd64/arm64 程序、安装入口、运维工具及 SHA256SUMS；截图为本地私有验收产物，不提交公开仓库。
- 完整 `make check` 通过（Go 全量测试/vet/架构、前端和部署检查）；修正目录句柄识别及并发测试后再次通过，全部 46 项部署测试通过。GitHub 发布与 CI 结果见下方最终交付记录。
- 未验证公网 ACME、ARM 真机；未部署云服务器或执行真实上游对话。

## 本机切换结果

- `127.0.0.1:10088` 已由 `codearts2api-managed.service` 运行 `v0.3.1`，PID 与受管 current 指向一致。
- 旧 `codearts2api-panel.service` 为 inactive / disabled；现用目录 `/home/white/codearts2api-managed/data`，备份在同级 `backups`，原源码目录数据仅为历史副本。
- 切换后逐项比较 accounts、keys、groups、users、settings、stats、usage_sequence、model_policies，全部一致；4 个账号、5 个密钥、3 个分组、1 个用户。
- doctor 返回 running、schema 10、findings 为空。保留原端口与 -no-refresh 行为；未修改云服务器及 SSH 隧道服务。
- 首次迁移被终端持有数据目录句柄误拦截，旧服务自动恢复。修正仅忽略目录句柄，仍拒绝实际状态路径/文件句柄，隔离复验后完成迁移。原目录约 306 MiB，完整压缩/恢复导致分钟级停机，不能宣传无停机或固定秒级升级。
- 新 bootstrap 使用真实候选包字节，经同一摘要验证入口完成真实 systemd 安装、首次向导、登录和二进制核验；网络层为本地发布夹具，公开下载另行验证。

云端检查补充：v0.3.1 Release 应用和部署检查通过，但并行普通 CI 在既有 internal/catalog/presets.go 的 gofmt 检查失败。修正纯格式后使用 v0.3.2 新标签，不改写 v0.3.1 标签。

跨架构构建补充：v0.3.1 云端镜像构建在 QEMU ARM 前端测试中超时。Dockerfile 调整为构建机原生执行前端检查与构建，Go 使用 TARGETOS/TARGETARCH 交叉编译；最终运行镜像仍为目标架构，未放宽测试时限或移除测试。修正使用新标签 v0.3.3 验证，不移动已有标签。

CI 环境补充：普通 CI 的 ubuntu-latest 已升级为 Ubuntu 26.04，当前锁定 Playwright 无对应浏览器安装支持。浏览器验收 runner 固定为 Ubuntu 24.04；未改应用代码或降低浏览器测试要求。v0.3.3 Release 工作流已完整通过，最终普通 CI 使用相同应用源码加 runner 修正重新验证。

Windows 并发验收补充：成长执行、Buddy 领养的并发测试也改为预先登录后复用会话，避免把测试目标之外的慢密码验证并发限制混入业务并发断言。三组并发测试 race 各重复 10 次通过；未改变生产认证或任务逻辑。

浏览器时序补充：云端 Chromium 在刷新设置页后、Vue 挂载前执行重载断言，原谓词直接读取空节点并抛异常。测试改为等待节点和值出现；未放宽保存值和继承状态断言。GitHub 构建的 v0.3.3 包在本机全部 15 组页面场景通过，修正后的设置页测试使用相同 Chromium 再重复 3 次通过。

## 最终交付与公开入口

- [Release 工作流](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/runs/41461830532) 成功：生成 v0.3.3 的两个架构包、独立 install.sh、panelctl.py、SHA256SUMS，以及 GHCR 双架构镜像。
- [main CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/runs/41465800244) 与 [develop CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/runs/41465800775) 全部通过：Linux 全量测试/race、Windows 构建/测试、部署单元测试、15 组 Chromium 页面验收。CI 使用 `defad5d`；与 v0.3.3 的应用源码一致，差异仅为 CI 环境、测试与验收记录。
- v0.3.3 已公开并标记为最新稳定版，五个附件齐全。两个程序包及安装工具的四项 SHA-256 全部匹配；amd64 实际执行报告 v0.3.3，arm64 ELF 架构正确但未在 ARM 真机执行。
- 从公开标签下载 install.sh 并核对发布摘要；匿名执行 `check` 成功找到 v0.3.3，匿名 `download` 从正式 Release 成功下载程序和工具并通过摘要校验，下载结果为本地私有产物 `artifacts/public-v0.3.3/`。
- 真实浏览器分别启动隔离的 v0.3.1/v0.3.3 实例，查询实际公开 Release：旧版显示“有新版本”、新版显示“当前已是最新正式版本”。检查不改变设置脏状态，无页面异常；服务端临时 GitHub 认证仅存在于隔离进程环境，结束后随进程清除。
- GHCR 的 v0.3.3 清单可匿名读取，包含 linux/amd64、linux/arm64。镜像构建和容器运行检查通过；本机 Docker daemon 再次从公开 GHCR 拉取时遇到 EOF/连接超时，未将该拉取过程记为通过。

## 本机最终状态

- 使用上述公开 install.sh 实际执行 `upgrade`，从 v0.3.1 升至 v0.3.3，升级前生成完整备份；同目录保留迁移和升级两份备份。
- 当前访问地址为 `http://127.0.0.1:10088`；实际 PID 404553，工作目录 `/home/white/codearts2api-managed`，current 指向 `releases/v0.3.3`，实际运行二进制 SHA-256 与公开包中程序一致。
- `codearts2api-managed.service` 为 active / enabled；旧源码服务保持 inactive / disabled。data 位于 `/home/white/codearts2api-managed/data`，backups 位于同级目录。
- 升级前后 accounts、keys、groups、users、settings、stats、usage_sequence、model_prices、consumer 的规范 JSON 摘要全部一致：4 个账号、5 个密钥、3 个分组、1 个用户。
- 发布附件中的 panelctl.py 实际运行 doctor：status=running、schema=10、findings=[]。端口和 -no-refresh 保留；本轮未部署云服务器、申请公网证书或调用真实推理上游。
