# 跨机器接入 Pi、Codex、Claude Code

## 能自动适配到什么程度

服务端统一提供三种推理协议和账号池，客户端只使用网关 API key，不接触华为账号凭据。
`GET /v1/client-profile?model=deepseek-v4.1-flash` 返回版本化的推荐参数，启动器每次运行时读取它。
默认 DeepSeek 配置为 65,536 上下文、8,192 输出；这是保守的已验证配置，不是上游最大上下文声明。
Trae CN 的 `deepseek-v4.1-flash` 自动提供 65,536 上下文、4,096 输出的保守客户端建议；未开启服务端摘要保护，也不代表已验证 Trae 的最大上下文。

Pi 的摘要适配已经迁到服务端：在标准摘要 system 提示且没有工具定义时关闭 DeepSeek 额外思考并加入精简约束。普通编程请求不改写。这个逻辑在协议转换之后应用，作用于收到的标准 Chat 消息，标准请求由服务端处理；大段历史的客户端恢复适配由启动器自动安装，无需手工复制扩展。

客户端本地的上下文容量、压缩阈值和接入凭据仍需要设置。服务端无法修改第三方工具的本地设置，也不能恢复已被客户端压缩/丢弃的历史。因此这里提供一键启动器，而不是宣称任何版本的工具只填 URL 就能完全自动兼容。

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
- 未验证的模型返回 422，不自动套用 DeepSeek 的参数，也不自动将未知上限填成 1M。
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
