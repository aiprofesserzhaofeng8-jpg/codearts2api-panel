# 跨机器接入 Pi、Codex、Claude Code

## 能自动适配到什么程度

服务端统一提供三种推理协议和账号池，客户端只使用网关 API key，不接触华为账号凭据。
`GET /v1/client-profile?model=deepseek-v4.1-flash` 返回版本化的推荐参数，启动器每次运行时读取它。
默认 DeepSeek 配置为 65,536 上下文、8,192 输出；这是保守的已验证配置，不是上游最大上下文声明。
Trae CN 的 `deepseek-v4.1-flash` 自动提供 65,536 上下文、4,096 输出的保守客户端建议；未开启服务端摘要保护，也不代表已验证 Trae 的最大上下文。

R6 增加模型用途与客户端参数依据。共享 `glm-5.3-flash` 可在全部来源有依据时提供 32,768 上下文、4,096 输出的建议；原生官方规格、来源目录、已保存策略与实测建议保持分离。专用补全、跳转、重写、图片生成等入口不自动导出到聊天客户端，显式网关调用仍按原有权限处理。缺参来源会给出原因，不自动套用其他来源的参数。详细证据见 [R6 验收](acceptance/CLIENT-METADATA-R6-2026-11-07.md)。

Pi 的摘要适配已经迁到服务端：在标准摘要 system 提示且没有工具定义时关闭 DeepSeek 额外思考并加入精简约束。普通编程请求不改写。这个逻辑在协议转换之后应用，作用于收到的标准 Chat 消息，标准请求由服务端处理；大段历史的客户端恢复适配由启动器自动安装，无需手工复制扩展。

客户端本地的上下文容量、压缩阈值和接入凭据仍需要设置。服务端无法修改第三方工具的本地设置，也不能恢复已被客户端压缩/丢弃的历史。因此这里提供一键启动器，而不是宣称任何版本的工具只填 URL 就能完全自动兼容。

WorkBuddy 的图片能力现在从上游 `supportsImages` 进入客户端推荐；Pi 配置会对明确支持的型号写入 `input: ["text", "image"]`。升级后请先刷新模型目录，再重新运行启动器生成配置。旧的 `input: ["text"]` 不会仅因网关升级而自动改变。详见 [图片输入与网络恢复](WORKBUDDY-MULTIMODAL.md)。

TeleAgent CN 的五项工作参数随[部署样例](../deploy/teleagent-model-policies.json)提供。升级前运行 `python3 scripts/migrate-teleagent-policies.py`，在本地终端输入管理员登录名和密码。也可在“模型 → 客户端兼容与压缩策略”按精确来源保存，已有管理员策略继续使用。Lite 的输出建议为 8192，实测完整生成记录为 1024。更新网关后运行 `python3 scripts/connect-client.py pi --sync-models` 同步 Pi。[测试记录](acceptance/TELEAGENT-LIMITS-2026-11-23.md)

## 使用方法

需要 Python 3 和对应客户端已安装。将 `scripts/connect-client.py` 拷贝到客户端机器即可，它只依赖 Python 标准库。

```bash
# URL 可以是 HTTPS 服务地址，或 SSH 转发后的本机地址。
python3 connect-client.py pi --url https://your-gateway.example
python3 connect-client.py codex --url https://your-gateway.example
python3 connect-client.py claude --url https://your-gateway.example
```

不提供环境变量时，启动器会隐藏输入地询问调用密钥；密钥不写入配置。也可以预先安全地设置 `CODEARTS_API_KEY`，使用 `CODEARTS_BASE_URL` 指定默认地址。不要使用管理员密码或上游账号 token。

仅验证接入和生成配置，不发模型请求：

```bash
python3 connect-client.py pi --url http://127.0.0.1:10087 --check
```

`--` 后面的参数传给原客户端，例如：

```bash
python3 connect-client.py pi -- --print '你好'
python3 connect-client.py codex -- exec '你好'
python3 connect-client.py claude -- --print '你好'
```

每次用此启动器打开客户端，推荐参数会自动更新。直接运行原来的 `pi/codex/claude` 不会自动使用这套配置。

同步现用 Pi 的 codearts-local 目录：

```bash
python3 scripts/connect-client.py pi --url http://127.0.0.1:10087 --sync-models
```

此命令使用该 provider 已有调用密钥，只读取一次模型列表。已知建议会同步上下文、输出及每模型压缩参数，并备份涉及的配置文件；其他 provider、默认模型、未知参数的已有条目保持，跳过项在报告中说明。需要支持 R6 的网关才能获得新增建议；本轮源码尚未部署，旧网关的缺参状态不会因客户端脚本更新而消失。

## 各客户端处理

| 客户端 | 推理协议 | 自动配置 |
| --- | --- | --- |
| Pi | `/v1/chat/completions` | 专用 `codearts` provider、上下文/输出上限、每模型压缩 reserve=16384/keep=12000，自动安装受控分段摘要恢复适配 |
| Codex | `/v1/responses` | 自定义 provider、API key 环境变量、关闭 WebSocket、上下文 65536、自动压缩阈值 45000、禁用托管网页搜索 |
| Claude Code | `/v1/messages` | 基础地址、模型及子模型映射、上下文/输出限制、关闭 1M 假定、清除继承的其他供应商路由标志；自动省略当前不支持的 adaptive / effort 控制 |

Pi 配置和会话保存在 `~/.config/codearts2api/clients/<服务地址哈希>/pi/`，不会覆盖原来的 `~/.pi/agent/`。配置文件只引用环境变量，不含密钥。
Codex 使用进程级 `-c` 参数，不改写原 config.toml 或认证文件。
Claude Code 使用 `--setting-sources ''`，防止旧的用户/项目配置把请求转到其他服务；管理员策略仍由客户端处理。其原有项目 hooks、个人设置不会自动载入这一专用启动过程。未增加权限绕过参数。
启动器根据 profile 的 `claude_code` 能力声明，在子进程中自动设置 `CLAUDE_CODE_EFFORT_LEVEL=auto`、`MAX_THINKING_TOKENS=0`，不要求用户手动设置或修改全局配置。含义是客户端不要求网关尚不支持的思考控制，上游仍采用原有默认行为；不保证上游关闭思考。绕过启动器、直接显式发送不支持的参数仍返回 400，不静默丢弃用户意图。

## 部署与功能边界

- 本机默认监听 127.0.0.1。远程访问建议 SSH 转发或 HTTPS 反向代理，不能把另一台机器上的 127.0.0.1 当成服务器地址。
- 对外部署必须重新审视管理面自动解锁，参阅安全审计报告；客户端启动器不替代服务端安全部署。
- 上游不支持的托管工具、Responses 存储、previous_response_id、原生 `/responses/compact` 和 WebSocket 并未被“配置”出来。启动器尽量选用兼容的调用方式，不能保证客户端所有新功能可用。
- Codex/Claude Code 的完整长程压缩循环需分别验证；Pi 本地摘要已用真实历史验证。不能将一次短问答或工具测试当成完整客户端兼容认证。
- Claude Code 官方不支持通过第三方网关使用非 Claude 模型。本项目提供的是社区协议兼容，实际行为取决于客户端版本。
- 普通聊天模型精确匹配 models.dev 后，自动推荐依次查找 OpenCode Zen、OpenCode Go 和原厂条目，使用同型号的上下文和最大输出。手动配置优先。缺少公开规格时沿用渠道目录、已有来源建议或兼容模板。统一模型按授权来源取公共值。
- 公开资料取自 models.dev，仅在既有模型目录刷新时下载，成功缓存 6 小时，失败 30 分钟后再试，最长增加 5 秒刷新等待；失败保留旧资料或使用兼容模板。请求不包含账号、密钥、对话、文件或模型清单；普通推理不查询该站点。
- 面板和 profile 区分上游目录、实测建议、公开资料和兼容默认参数；后两者不是平台实测最大值，不自动扩大图片或工具能力。模型页可手动调整参数；没有启用新的服务端摘要能力。缺失来源、身份变化等仍可能返回 `missing_limits`，专用或用途未确认入口返回 `unsupported_purpose`，不会自动导出聊天配置。
- Windows/macOS 的完整客户端安装流程未在本机验证。当前运行验证环境为 Linux。

## 验证证据

服务端回归：`go test -race ./...`、`go vet ./...`。
启动器回归：`python3 scripts/test_connect_client.py`。
真实客户端结果：`artifacts/mainstream-clients-check.json`、`artifacts/mainstream-clients-tools-check.json`。
不加载客户端摘要改写的真实 Pi 压缩测试：`artifacts/pi-server-compaction-live-check.json`。

参考：
- Pi 已安装版本的 models.md、settings.md（`/model` 重读配置、per-model compaction）。
- https://developers.openai.com/codex/config-reference/
- https://code.claude.com/docs/en/llm-gateway-connect
- https://code.claude.com/docs/en/model-config

本机验证版本：Codex CLI 0.164.0、Claude Code 2.1.238。三种客户端短问答通过；Pi 与 Claude Code 的只读文件工具闭环通过。Codex 工具调用到达执行层，但本机 bwrap 网络命名空间权限与 MCP 审批策略阻断了读取，不能声称其工具闭环测试通过。

模型参数现在可在「模型 → 客户端兼容与压缩策略」修改，启动器每次运行获取新配置。摘要完整性保护和同账号最多一次截断重试详见 SUB2API-ALIGNMENT.md。不支持的原生 Responses 子接口返回 501，不能依赖它执行原生 compact。

## 大段历史与 Linux 沙箱修复

启动器内置 Pi 恢复适配代码，单文件复制仍可用。普通阈值和大段历史回填两类场景均完成三轮真实自动压缩；已有本机 Pi 扩展也已更新，运行中的会话用 `/reload` 加载。显式 `--no-extensions` 会禁用这层客户端恢复。

Linux Codex 在正常启动前会先做免费原生沙箱检查。Ubuntu 缺失 bwrap 用户命名空间 profile 时会提前停止，不先花 Token 尝试失败命令。检查：`python3 scripts/connect-client.py codex --check --check-sandbox`。管理员修复和证据见 [修复记录](acceptance/REMEDIATION-2026-10-26.md)。

### Sub2API 模型输入与输出参考值

| 网关公开名 | Sub2API 型号 | 最大输入 Token | 最大输出 Token |
|---|---|---:|---:|
| `gpt-6.1-terra` | `gpt-6.1-terra` | 922,000 | 128,000 |
| `claude-haiku-4.5` | `claude-haiku-4-5` | 200,000 | 64,000 |
| `claude-sonnet-4.5` | `claude-sonnet-4-5` | 200,000 | 64,000 |

总上下文窗口需单独查证。以上数值分别来自 `max_input_tokens` 和 `max_output_tokens`。[Sub2API 模型规格表](https://github.com/Wei-Shaw/sub2api/blob/main/backend/resources/model-pricing/model_prices_and_context_window.json)

### models.dev 客户端参数映射（2026-11-23）

客户端推荐直接采用 OpenCode 使用的 models.dev 数据。同名条目依次查找 OpenCode Zen、OpenCode Go、原厂目录；华为和火山目录也参与查找。以下别名仅用于读取上下文和输出数值，不修改公开模型名或上游请求的型号。以后 models.dev 出现完全同名条目时，完全同名优先。

| 网关型号 | models.dev 提供方 / 型号 | 上下文 Token | 目录最大输出 Token |
|---|---|---:|---:|
| `openpangu-2.0-flash` | `huawei-maas/openpangu-2.0-flash` | 524,288 | 131,072 |
| `openpangu-2.0-pro` | `huawei-maas/openpangu-2.0-pro` | 524,288 | 131,072 |
| `deepseek-v4-flash-official` | `opencode/deepseek-v4-flash` | 1,000,000 | 384,000 |
| `deepseek-v4-pro-official` | `opencode/deepseek-v4-pro` | 1,000,000 | 384,000 |
| `deepseek-v4.1-flash-1017` | `huawei-maas/deepseek-v4.1-flash-1017` | 1,000,000 | 384,000 |
| `doubao-seed-2.0-code` | `volcengine/doubao-seed-2-0-code-preview-260215` | 262,144 | 131,072 |
| `doubao-seed-2.1-pro` | `volcengine/doubao-seed-2-1-pro-260628` | 1,024,000 | 256,000 |
| `doubao-seed-2.1-turbo` | `volcengine/doubao-seed-2-1-turbo-260628` | 256,000 | 256,000 |
| `qwen-3.7-plus` | `opencode-go/qwen3.7-plus` | 1,000,000 | 65,536 |

数值取自 [models.dev](https://models.dev/api.json)，由已有的目录刷新流程更新。测试使用[本次提取的规格记录](../backend/internal/catalog/testdata/client-reference-limits-20261123.json)。豆包未带日期的公开名按表中快照推荐；此表定义客户端默认值，不要求先验证渠道能接收最大长度。管理员手动策略仍优先。统一模型的多个授权来源取公共值。

Turbo 的目录输出等于整个窗口，既有客户端压缩策略需留出 4,096 Token 预留和至少 1,024 Token 输入，因此当前导出输出为 250,880，窗口仍为 256,000；其他八项导出输出与表中一致。

本次查找的 12 项中，`codearts-speed`、`openpangu-2.1-pro`、`glm-5.2-sft-harmony` 在 models.dev 没有对应条目，继续沿用既有配置。三个缺项不影响上表九项应用。部署新版本并成功刷新模型目录后，再运行 `python3 scripts/connect-client.py pi --sync-models` 更新现用 Pi；仅修改源码不会改变正在运行的服务或客户端。

### 平台模型名称与厂商窗口对照（2026-11-23，资料核查记录）

数值单位为 Token，K、M 沿用厂商标法。最大输出与历史输入共享总窗口。表中注明候选或快照的行，使用前需核对渠道实际返回的模型版本。破折号表示该来源未确认这一项。

| 网关公开名 | 厂商型号或适用条件 | 上下文 | 最大输出 | 来源 |
|---|---|---:|---:|---|
| `deepseek-3.2` | 来源为 Kiro 美东。Kiro CLI 曾公布该地区窗口，当前账号目录值待核 | 128K（Kiro CLI 文档） | — | [Kiro 型号表](https://kiro.dev/docs/cli/chat/model-selection/)、[当前型号页](https://kiro.dev/docs/models/available-models/) |
| `deepseek-v4-flash-official` | 对应官方 V4 Flash 旧 API 名时，请求由 V4.1-Flash-1017 服务 | 1M | 384K | [DeepSeek API](https://api-docs.deepseek.com/quick_start/pricing/) |
| `deepseek-v4-pro-official` | 对应官方 V4-Pro-0813 时 | 1M | 384K | [DeepSeek API](https://api-docs.deepseek.com/quick_start/pricing/) |
| `deepseek-v4.1-flash-1017` | DeepSeek-V4.1-Flash-1017 | 1M | 384K | [DeepSeek API](https://api-docs.deepseek.com/quick_start/pricing/) |
| `doubao-seed-2.0-code` | 历史参考快照 2-0-code-preview-260215，最大输入 224K | 256K | 128K | [火山方舟模型表](https://console.volcengine.com/ark/region%3Acn-beijing/docs/model-list) |
| `doubao-seed-2.1-pro` | 260628 / 260915 两个快照 | 256K / 1024K | 256K | [火山方舟模型表](https://docs.volcengine.com/docs/ark/model-list?lang=en) |
| `doubao-seed-2.1-turbo` | 260628 快照 | 256K | 256K | [火山方舟模型表](https://docs.volcengine.com/docs/ark/model-list?lang=en) |
| `glm-5.2-sft-harmony` | 鸿蒙渠道候选 GLM-5.2-ArkTS-SPARK；码道列出的该型号窗口 | 202.8K | — | [码道 IDE](https://support.huaweicloud.com/usermanual-codeartsagent/codeartsagent_ug_0009.html) |
| `kimi-k2.6-vl` | 候选多模态 Kimi-K2.6，文本配置 | 262,144 | — | [Moonshot 配置](https://huggingface.co/moonshotai/Kimi-K2.6/blob/main/config.json) |
| `kimi-k2.8-preview` | Kimi Code 的 K2.8 Preview，官方入口 kimi-for-coding | 最高 1M | — | [Kimi Code](https://www.kimi.com/en/help/kimi-code/models) |
| `kimi-k3-1` | WorkBuddy 目录分别列出 kimi-k3-1 和 kimi-k3.1-pro，两者映射关系待核 | 1M（仅候选 K3.1 Pro） | — | [Kimi 开放平台](https://platform.kimi.com/) |
| `Qwen3-VL-235B` | 未标 Instruct / Thinking。Instruct 权重配置为 262,144 个位置，百炼 Instruct API 最大输出见右列 | 262,144（权重） | 32,768（Instruct API） | [Qwen 配置](https://huggingface.co/Qwen/Qwen3-VL-235B-A22B-Instruct/blob/main/config.json)、[百炼规格](https://help.aliyun.com/zh/model-studio/qwen3-vl-235b-a22b-instruct) |
| `qwen-3.7-plus` | qwen3.7-plus | 1,000,000 | 131,072 | [百炼规格](https://help.aliyun.com/zh/model-studio/qwen3-7-plus) |
| `qwen3-coder-next` | 同名百炼 API，最大输入 204,800 | 262,144 | 65,536 | [百炼规格](https://help.aliyun.com/zh/model-studio/qwen3-coder-next) |
| `openpangu-2.0-flash`、`openpangu-2.0-pro`、`openpangu-2.1-pro` | 码道 IDE 对三个型号共同公布的窗口 | 524.3K | — | [码道 IDE](https://support.huaweicloud.com/usermanual-codeartsagent/codeartsagent_ug_0009.html) |
| `codearts-speed` | 对应码道「智速模式」时 | 1.05M | — | [码道 IDE](https://support.huaweicloud.com/usermanual-codeartsagent/codeartsagent_ug_0009.html) |

#### Trae 和 WorkBuddy 渠道别名

| 网关公开名 | 可核对的规格和剩余缺口 |
|---|---|
| `trae-custom_model_tokenpony_deepseek_v4_1_flash` | 名称指向 DeepSeek V4.1 Flash，官方 1017 版为 1M / 384K。TokenPony 路由的快照与上限待核。[DeepSeek](https://api-docs.deepseek.com/quick_start/pricing/) |
| `trae-custom_model_tokenpony_glm_5_3` | 名称指向 GLM-5.3，官方权重窗口 1,048,576 个位置。TokenPony 最大输出待核。[智谱配置](https://huggingface.co/zai-org/GLM-5.3/blob/main/config.json) |
| `trae-custom_model_tokenpony_qwen3_8_max` | 名称指向 Qwen3.8-Max，百炼规格为 1M / 128K。TokenPony 上限待核。[百炼规格](https://help.aliyun.com/zh/model-studio/text-generation-model/) |
| `trae-custom_model_1M`、`trae-custom_model_1M_text`、`trae-custom_model_doubao_1M` | 名称标示 1M 档位。实际快照、渠道窗口及最大输出待核。 |
| `trae-custom_model_doubao_256k` | 名称标示豆包 256K 档位。具体快照和输出上限待核。 |
| `trae-custom_model_claude`、`trae-custom_model_gemini`、`trae-custom_model_kimi`、`trae-custom_model_gpt-5`、`trae-custom_model_gpt-6` | 名称标示系列，具体快照、上下文与输出待渠道确认。 |
| `trae-custom_model_deepseek_chat`、`trae-custom_model_deepseek_reasoner`、`trae-custom_model_deepseek_v4` | 渠道别名所对应的具体版本和输出模式待核。 |
| `trae-custom_model_no-fc`、`trae-custom_model_placeholder` | 能力标记或占位名称，底层型号与上限待核。 |
| `workbuddy-hy3` | 对应腾讯 Hy3 时，窗口 256K，最大输入 192K，最大输出 128K。[腾讯 TokenHub](https://cloud.tencent.com/document/product/1823/130051) |
| `workbuddy-hy4flash`、`workbuddy-hy4flash-free` | 对应腾讯 Hy4 Flash 时，窗口 1M，最大输入 960K，最大输出 64K。免费档的渠道限制待核。[腾讯 TokenHub](https://cloud.tencent.com/document/product/1823/130051) |
| `workbuddy-hy4std`、`workbuddy-hy4free` | 候选 Hy4 的官方规格为 1M / 64K。两个 WorkBuddy 档位的实际映射待核。[腾讯 TokenHub](https://cloud.tencent.com/document/product/1823/130051) |
| `workbuddy-hy3-x`、`workbuddy-space-bunny` | 渠道标识，具体版本和数值待核。 |
| `workbuddy-auto` | 自动路由，有效上限取决于所选来源。 |
| `workbuddy-codewise-completions`、`workbuddy-codewise-default-model-v2`、`workbuddy-codewise-jump`、`workbuddy-codewise-rewrite`、`workbuddy-nes-gf` | 其中 completions、jump、rewrite 分别对应补全、跳转和改写。default-model-v2 与 nes-gf 的用途在网关目录中标记为 unknown。[项目渠道目录](../backend/internal/catalog/default_models.json) |
| `workbuddy-hunyuan-image-alpha` | 网关目录标记为图像生成入口，文本聊天最大输出不适用。[项目渠道目录](../backend/internal/catalog/default_models.json) |

#### 45 个待核条目的渠道复核

按公开名逐项比对 11 月 7 日的来源级目录，33 项采用网关兼容默认值，6 项标为 unknown，另 6 项当时尚未收录。后六项分别是 openpangu-2.1-pro、三个 Trae TokenPony 槽位和两个 WorkBuddy hy4flash 档位。11 月 19 日的统一模型快照记录的是客户端建议值。[来源级快照](../artifacts/sub2api-metadata-2026-11-07/results.json)、[统一模型快照](../artifacts/astra-protocol-recheck-1119/catalog.json)

- **DeepSeek 3.2** 的来源是 Kiro 美东。AWS Bedrock 单独公布了 164K 窗口和 8K 最大输出。Kiro 的区域型号表列过 128K，Kiro 当前型号介绍未列最大输出。配置 Kiro 时应以该账号 ListAvailableModels 返回的 tokenLimits 为准。[Kiro 型号表](https://kiro.dev/docs/cli/chat/model-selection/)、[Bedrock 模型卡](https://docs.aws.amazon.com/bedrock/latest/userguide/model-card-deepseek-deepseek-v3-2.html)
- **三个 openPangu 型号**在码道 IDE 的窗口均为 524.3K。华为 MaaS 文档给出的 2.0 两款 32K、2.1 Pro 64K 均为输出参数默认值，取值上限另见 MaaS 控制台。码道渠道最大输出仍待确认。[码道上下文表](https://support.huaweicloud.com/usermanual-codeartsagent/codeartsagent_ug_0009.html)、[MaaS 参数说明](https://support.huaweicloud.com/model-call-maas/model-call-021.html)
- **两个 DeepSeek Official、三个豆包入口和 17 个 Trae 自定义槽位**均需区分渠道配置与厂商快照。网关的 Trae CN / SOLO 目录适配器只解析配置名和显示名。TRAE 企业版文档给出的内置模型普通档为 200K，部分模型开启 Max 后为 1M。自定义模型还可设置模型 ID 和输入输出参数。这里缺少实际账号的槽位配置和模式，尤其不能把名字中的 1M 换算成最大输出。[目录解析](../backend/internal/trae/client.go)、[TRAE 模型设置](https://docs.trae.cn/trae-enterprise_model-settings-for-trae-enterprise)
- **WorkBuddy** 官方模型表可确认 Hy3 的身份，且切换模型后会采用新模型的上下文设置。腾讯 TokenHub 公布的 Hy3、Hy4 参数可供对应版本参考。确认当前账号每个入口的参数，需要 WorkBuddy 的 /v3/config 对该 ID 返回 maxInputTokens 和 maxOutputTokens，其中前者表示最大输入。此次未登录读取返回的 models 为空。[WorkBuddy 型号说明](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Model)、[网关读取字段](../backend/internal/workbuddy/client.go)


### Pi 项目参数偏好

当前 140 个模型的上下文、最大输出、压缩预留与触发阈值见 [Pi 模型参数配置表](PI-MODEL-PARAMETERS.md)。

仓库内启动器读取 `scripts/pi/model-settings.json`：上下文最低 131,072 Token，默认最大输出 65,536 Token，`maxOutputTokens` 可按精确名称覆盖（`glm-5.3` 为 131,072）。精确名称的窗口覆盖放在同一文件。它们是管理员选择的客户端工作参数，平台实际限制仍由上游决定。

同步时取已有窗口、网关窗口、项目最低值和精确覆盖值中的最大值；旧网关的 32K/64K 建议不会缩小已有长窗口。已有压缩预留及近期历史配置保留；缺失时才从网关补充。只有复制单文件启动器而未带此 JSON 时，沿用网关参数。要主动调小窗口，需要同时调整本机配置与该文件的覆盖值。

DeepSeek 无后缀 V4.1 Flash 的 1M 是此次管理员窗口配置，与 WorkBuddy 同名入口已有配置一致；它不表示所有同名来源都经过 1M 实测。TeleAgent 原始名称与统一名称使用相同工作窗口。自定义槽位不会按名字中的 1M 自动推断底层模型。


### 可维护的离线模型规格表

公开上下文与最大输出参考保存在 [models.dev.json](../backend/internal/catalog/modelmeta/data/models.dev.json)，与 Sub2API 使用同一 models.dev 数据源。没有在线结果或已保存参考时，网关用此表补充已发现模型的参数；不覆盖人工策略、不新增可用模型、不改变推理超时。

更新命令为 `python3 scripts/update-model-reference.py`，来源、离线生成方法和优先级见[维护说明](../backend/internal/catalog/modelmeta/data/README.md)。Pi 配置和渠道实测记录继续独立维护。
