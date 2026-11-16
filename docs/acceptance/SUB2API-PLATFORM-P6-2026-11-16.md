# P6 第一批：交付审计与本地制品验收

日期：2026-11-16。P6 工程交付验收第一批通过，**整个扩展计划尚未完成**：真实平台账号、部分客户端语义及正式发布仍有缺口。此次没有真实上游请求、没有改动生产状态或现有运行服务。

## 支持矩阵

“隔离通过”表示模拟上游/临时状态测试，不表示真实订阅或云权限通过。下表所有新增平台真实验收均待完成；TypeSafe 独立目录/页面按用户要求跳过。

| 平台/模式 | 已实现的接入 | 目录及边界 | 隔离状态 |
|---|---|---|---|
| DeepSeek | 官方 API Key | 原生目录、生成、余额 | 通过 |
| 智谱 | 官方 API Key、个人 Coding | 套餐端点独立；不含团队配置 | 通过 |
| Kimi | 官方 API Key、国内 Coding | 按量与套餐 scope 分开 | 通过 |
| MiniMax | 官方 API Key、国内 M Plan | 当前套餐额度真实响应结构待核实 | 通过 |
| Gemini API | AI Studio API Key | 原生目录、官方兼容生成端点 | 通过 |
| Gemini CLI | OAuth / Code Assist | 参考目录不是账号实时目录；Pi 兼容路径存在签名丢失限制 | 通过；客户端限制保留 |
| Antigravity | OAuth | 初始化、续期、目录、额度与生成 | 通过 |
| Grok Build | OAuth | 独立 Build 凭据与生成 | 通过 |
| OpenCode Zen / Go | API Key | 按量/订阅分开、原生协议分流 | 通过 |
| Command Code | 外部 API 可用套餐/按量 | 不包含不支持外部调用的 Go 套餐 | 通过 |
| Cline | API Key / Pass | 推理扩展字段网关可保留；Messages 实际客户端回传待验 | 通过；客户端限制保留 |
| TypeSafe | API Key | 原生 /v1/systemone 决策，不是聊天适配器 | 通过；独立页面跳过 |
| Bedrock | AK/SK、手工临时令牌 | 商业区域 Claude 基础型号/系统 profile；不含自动 STS/ARN/AssumeRole | 通过 |
| Vertex AI | 服务账号 / Gemini | 显式 model_ids；没有自动目录发现；不含 Claude/Express/ADC | 通过 |

详细协议、图片/工具与错误覆盖见各 P1～P5 验收记录；“同平台文字测试成功”不能替代图片、工具和自然续期验收。原有九平台没有据本表重新宣称真实通过。

## 本批已执行

1. 全量 make check：/tmp/codearts-p6-check.log。包含权限/保存/共享名称现有回归、编译检查、架构边界和部署脚本测试。
2. 10 个新增入口浏览器回归通过：Vertex、Bedrock、官方 API、Gemini CLI、Antigravity、Grok、OpenCode、Command Code、Cline、TypeSafe。报告 /tmp/codearts-p6-browser/browser-results.json。
3. Linux amd64 与 Windows amd64 实际打包，校验每个清单文件的大小/摘要及压缩包摘要；验证 local/dirty 来源标记。报告 /tmp/codearts-p6-package-audit.json。
4. Linux 真进程安装、健康、管理员登录、运行时禁止离线备份、备份恢复和非空账本单次重放通过：/tmp/codearts-p6-live-package.log。
5. 独立用户级 systemd 服务实际升级与可执行文件核对、私有备份、升级后登录、备份失败恢复启动、降级/错误摘要拒绝、坏候选版本停止并保留新凭据/账本通过：/tmp/codearts-p6-upgrade-acceptance.log。临时服务已清理，报告 existing_services_changed=false。
6. Dockerfile 实际构建通过，镜像 codearts2api:p6-local-20261116。镜像摘要 sha256:04c9e7aadd5bb86abfc247691db04034b13512f08be693fb42c8a1f9720a8802。
7. 独立 Compose 容器初装、健康、UID10001、回环端口、权限限制、停止卷备份恢复及重启登录通过：/tmp/codearts-p6-docker-acceptance.log。临时容器/卷已清理，existing_containers_changed=false。
8. 已有 local_files 来源摘要无漂移，Linux/Windows/Docker 保留本次第三方许可与来源说明。没有增加应用运行依赖或改动业务逻辑。

## 修复的验收缺陷

Windows 原生验收脚本仍要求旧版精确文件清单，会拒绝带新增平台许可的正确包。已补全清单，并抽出同一个 check_package 供非 Windows 的打包测试调用，防止以后只有打包测试通过、原生验收却因清单过时失败。

定向验证：/tmp/codearts-p6-windows-test.log。Windows 包仅完成交叉编译与清单/摘要验证，**没有在 Windows 执行 EXE 或 CMD**，不能标为 Windows 原生运行通过。

## 制品与版本边界

本地目录：/tmp/codearts-p6-packages/。候选版本 v0.4.14-local.202611160601，升级测试版本 v0.4.14-local.202611160602。都是当前未提交源码的验证制品，不是正式 Release，不用于宣称可替代现有生产版本。源码摘要写在各包 manifest.json；后续文档/测试变化不会追写已生成包，最终正式包必须由提交后的干净源码重新生成。

没有推送 GitHub、创建 tag/Release 或替换当前服务。镜像仅保存在本机。

## 仍待完成

- 每个已授权模式的真实认证、生成、工具/图片和自然续期；没有账号时保持未验证。
- Gemini CLI 的实际客户端签名兼容、Cline 其他 Messages 客户端扩展回传、MiniMax 当前套餐额度响应确认；本机 Pi Messages 已确认不兼容（见第三批）。
- Windows 主机原生运行、Linux arm64 构建/运行及对应制品验收。本批仅构建 Linux/Windows amd64 和当前主机 Docker 镜像。
- GitHub CI 对提交版本的验证、干净源码正式构建、Release 说明/附件、发布及运行服务升级，分别记录证据。
- TypeSafe 独立目录/页面已跳过；Vertex 自动模型发现及其他明确排除的云模式不是本批交付能力，不伪装已完成。

## 第二批：arm64 制品与发布门禁

2026-11-16：Linux arm64 本地包已构建：/tmp/codearts-p6-packages/codearts2api-panel-v0.4.14-local.202611160603-linux-arm64.tar.gz。校验压缩包摘要、全部清单文件大小/摘要及 ELF64 little-endian e_machine=183（AArch64），报告 /tmp/codearts-p6-arm64-audit.json。构建日志 /tmp/codearts-p6-arm64-package.log。

本机为 x86_64，Docker 执行 ARM Alpine 返回 exec format error；arm64 Docker 构建在目标镜像 RUN 阶段失败（/tmp/codearts-p6-arm64-docker.log）。这是执行环境缺少 ARM 支持，**未通过 arm64 运行验收**；没有修改宿主 binfmt、安装特权模拟器或改动现有服务。

新增可复用 .github/workflows/arm64-acceptance.yml，CI 和 Release 均调用：

- 原生 ubuntu-24.04-arm runner，先断言 uname -m=aarch64。
- 原生 Go 测试、真实 arm64 安装包、现有 live_acceptance 安装/登录/备份恢复/账本检查。
- 原生 arm64 Docker 构建及独立 Compose 验收，成功后上传 arm64-release。
- Release 同时依赖 Windows 与 ARM 作业；下载已验收的 arm64 制品，不再在 x86 发布作业里重新生成未运行验证的包。

Runner 标签依据 GitHub 官方说明：https://docs.github.com/en/actions/reference/runners/github-hosted-runners 。本地 YAML 解析、调用关系、Release 依赖、制品下载复用和 diff 空白检查通过；没有推送工作流，远端执行结果仍未知。应用源码无变动，无需重复整套业务测试。

第一批剩余项中的 arm64 “构建”现已完成，“运行”仍等待原生 runner 验收。Windows 原生、真实平台/客户端、正式提交发布和部署边界保持未完成。

## 第三批：实际 Pi 客户端离线两轮验收

2026-11-16：读取本机安装的 Pi 1.4.0 SDK 模块，使用自定义 fetch 返回合成 SSE。实际执行 stream → assistant 历史 → toolResult → 第二轮请求，核对发出的 assistant 消息。没有请求网关/真实上游，没有读取或修改 Pi 配置、账号或密钥。

| 客户端路径 | 测试结果 | 含义 |
|---|---|---|
| Pi Chat Completions / Cline reasoning_details | 两轮保留加密扩展与工具结果 | 此客户端路径的元数据回传通过；真实签名有效性未验 |
| Pi Messages / Cline reasoning_details | 两轮丢弃扩展，但保留原生 Anthropic signature 和工具结果 | 明确不兼容 Cline 扩展；不再记作“尚未测试” |
| Pi Chat Completions / Gemini extra_content.google.thought_signature | 工具与结果存在，签名丢失 | Gemini 签名工具链路未通过，换 CLI 来源不能修复客户端丢字段 |
| Pi Responses / Cline reasoning_details | 重新执行已有真实转换模块测试，扩展保留 | 只验证转换回传，不是 Responses 真实端到端生成 |

新增可重复执行的 scripts/verify-pi-metadata.mjs，参数是安装版本的 bundle/chunks 目录；脚本通过动态定位实际模块运行，不携带依赖副本，不修改用户配置。结果附模块名与 SHA256，避免将来 Pi 升级后沿用旧结论。

```sh
node scripts/verify-pi-metadata.mjs /path/to/pi-coding-agent/dist/bundle/chunks
```

本次报告：/tmp/codearts-p6-pi-metadata.jsonl；Responses 转换复验：/tmp/codearts-p6-pi-responses.json。脚本正常退出表示测试成功执行，不表示报告中的所有兼容字段为 true。后续依据具体协议字段选择兼容客户端路径；不增加网关签名存储、伪造签名、静默剥离推理或自动换模型。

更新 docs/CLINE.md 的客户端限制。没有更改应用源码/运行服务/客户端配置，没有推送或发布。其余真实账号、ARM/Windows 原生执行和发布待验项保持未完成。

## 第四批：上游兼容回归与发布说明

2026-11-16：发现 check-sub2api-compat.py 仍将修改后的转换文件与上游原始摘要直接比较，导致已记录的元数据适配无法执行原版回归。修复为 source_files 保留原始来源摘要、adapted_source_files 单独记录三个修改文件的本地摘要和原因。脚本独立下载并校验固定 SHA 的原始源码及测试，随后将原版测试与本地测试一起运行，下载的测试不会覆盖本地同名文件。未改动转换逻辑，没有把原始摘要替换成本地摘要。

固定 ba81c7fd758e425d67f856d601147ef0c5263d3f 的上游测试和本地回归一起通过 -race；日志 /tmp/codearts-p6-upstream-compat-final.log。新增本地完整性测试覆盖：未声明修改拒绝、声明后的进一步漂移拒绝、来源缺失/原因缺失拒绝；接入 make check。脚本网络访问仅用于公开上游代码和测试依赖，没有读取生产凭据。

发布说明草稿：../releases/PLATFORM-EXPANSION-DRAFT.md，明确支持范围、客户端限制、真实验证缺口和制品/部署边界。未选择新稳定版本、未提交推送、未发布或升级运行服务。实际发布仍需干净源码、远端 CI、平台真实验证记录及分别确认的交付状态。

## 第五批：兼容检查接入交付流水线

2026-11-16：之前的完整兼容校验只手动执行，没有 CI/Release 入口。现在 make check 包含离线本地摘要检查；CI 和 Release 都必须通过固定上游源码/测试摘要验证及原版+本地兼容回归（10分钟超时）。生产程序没有新增依赖、后台服务或网络请求。

本地验证：/tmp/codearts-p6-final-check.log（全量 make check）；/tmp/codearts-p6-ci-compat.log（固定上游及本地回归 -race）；workflow YAML 解析、检查步骤与超时存在性断言、diff 空白检查。上述为本地验证，未运行 GitHub CI。

计划首页同步当前结论：TypeSafe 独立目录按用户要求跳过；Pi Messages 丢失 Cline 扩展是已确认限制，不再写作从未验证。其余未完成项没有被降级或标成通过。

接下来的交付路径：提交并推送审阅后的源码 → 等待远端 Linux/Windows/ARM 验收 → 对已授权新增平台进行真实测试并记录受阻项 → 使用干净提交构建正式版本与 Release。当前没有提交/推送、没有发布、没有替换运行服务。

## GitHub 推送保护处理

GitHub 拒绝包含 Google OAuth Client ID/Secret 的未发布提交。按用户要求移除 Gemini/Antigravity 内置客户端值，改为部署环境配置；测试使用合成值，Compose 增加明确环境变量透传，systemd 配置方法写入平台文档。不关闭保护、不混淆字符串、不申请自动绕过。未修改生产服务或填入真实环境变量。

变更影响：这两个浏览器 OAuth 模式及续期现在需要部署配置；Gemini API Key 不受影响。源码导入/刷新测试以及缺少配置立即失败测试覆盖该行为。被拒绝的提交需 amend，不能只新增删除提交而让旧值留在待推送历史。验证日志 /tmp/codearts-oauth-env-check.log、/tmp/codearts-oauth-env-race.log。
