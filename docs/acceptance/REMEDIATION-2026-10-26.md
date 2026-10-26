# Pi 压缩失败与 Codex 沙箱阻塞的修复记录

2026-10-26。对应 R05 / Q-01 与 C03 / Q-02。原始失败证据保留；修复验证另存到本次验收的 fixes 子目录，不覆盖失败历史。

## Pi：已实现并真实复测

Pi 1.4.0 的 provider-neutral `clampMaxTokensToContext()` 会按估算上下文减去安全预留，最小保留 1 个输出 Token。大段历史回填后，摘要请求本身也经过这个估算；原失败记录的两次尝试都只生成了 1 个输出 Token，HTTP 422 后未建立新检查点。实际输入 Token 少于估算值，并不使客户端自动恢复输出预算。

修复在 Pi 的 `session_before_compact` 阶段执行，限定 `codearts` / `codearts-local` 的 `deepseek-v4.1-flash`：

1. 从准备压缩的消息、上一检查点和被切开的回合前缀构建文本；不携带图片二进制或隐藏思考块。
2. 大段文本按 UTF-8 字节边界分段，每段最多 24 KiB；先摘要每段，再合并检查点。
3. 通过 Pi 已配置的 ModelRegistry 原始流接口发送独立、受控输出预算的摘要请求，避免再次套用那次过大的估算。调用密钥由 Pi 运行时解析，不写入扩展。
4. 所有分段和合并都必须正常 stop、有正文、无工具调用。任一步失败、取消或超额时不返回替代检查点，由 Pi 保留原会话。
5. 每次恢复最多 16 个模型请求、最多 3 层归并；源文本另限 512 KiB，并在调用前拒绝明显超过分段请求上限的历史。每段输出最多 1536，最终输出最多 4096，仍受模型上限和网关预算限制。网关自己的摘要截断重试仍最多一次；客户端没有额外无限重试。
6. 合计已报告用量，保留 `firstKeptEntryId`、文件修改记录、检查点及必要标识。普通编程请求的 Token 上限不被抬高。

启动器已经内置这份适配并自动安装，复制一个 connect-client.py 到其他机器仍可使用；停用摘要适配时会移除托管的扩展。当前本机已有扩展也已更新。正在运行的 Pi 执行 `/reload` 或重新启动后载入。

真实测试使用同一合成 Pi 会话，每轮逐步增加或一次性导入历史，然后调用公开的 session.prompt()，由 Pi 自动触发压缩。正常阈值和突发历史各连续 3 轮通过，约束及全部里程碑标记保留；原失败中的第二轮现已完成恢复。安装适配后的真实只读工具闭环也通过。另有 Unicode 分段、取消、拒绝半截摘要、调用上限、用量合计、文件记录和普通请求隔离测试。

代价：大段历史会产生多次摘要调用，增加耗时和额度消耗；不能把这个修复理解为无限上下文。超过恢复界限仍需先缩减历史。

## Codex：系统配置已准备，等待管理员加载

本机为 Ubuntu 24.04.3，bubblewrap 0.9.0，`kernel.apparmor_restrict_unprivileged_userns=1`，缺少 `/etc/apparmor.d/bwrap-userns-restrict`。本机无模型探针和真实 Codex 均在命令执行前出现 `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted`。直接 user namespace 探针也失败。

按照 [OpenAI 官方沙箱文档](https://learn.chatgpt.com/docs/sandboxing)，此环境应安装并加载 Ubuntu 提供的 bwrap AppArmor profile。已从签名校验通过的 Ubuntu noble-updates 软件源取得官方 apparmor-profiles 软件包，提取该 profile 并通过本机 apparmor_parser 的不加载内核语法检查。文件哈希及软件包来源见 CODEX-SANDBOX-FIX.json 和 fixes 目录的 codex-system-fix.json。

脚本默认仅检查：

```bash
python3 scripts/install-codex-sandbox.py
```

管理员执行以下命令会备份已有配置、安装这个明确的官方 profile 并加载；随后降回原用户验证原生只读沙箱的读取、写入阻断和网络阻断，再使用独立受限密钥进行一次真实 Codex 读工具闭环，成功后自动更新 C03：

```bash
cd codearts2api-panel
sudo python3 scripts/install-codex-sandbox.py --apply --retest
```

脚本不调整全局 user namespace sysctl，不为 Codex 增加绕过沙箱的标志。当前 `sudo -n true` 返回需要密码，当前会话无法执行内核 profile 加载，因此 C03 保持 BLOCKED；不能用 profile 语法通过代替实际工具执行通过。

启动器还加入了免费原生沙箱预检：Linux Codex 的正常启动先验证沙箱能执行一个空操作，失败时在生成模型请求前停止，并给出修复入口，避免模型反复尝试失败命令而消耗 Token。`--check` 继续只验证配置；`--check --check-sandbox` 可额外验证沙箱。已验证失败预检前后的模型请求计数一致。

## 持续观察

本次修复不要求重启网关，原 24 小时观察继续。真实账号自动续期已经被观察到；长期稳定性仍等待满 24 小时。本机实时报告为 `QUALITY-REPORT.md`；仓库中的验收结果见 [QUALITY-SNAPSHOT.md](QUALITY-SNAPSHOT.md)。

公开源码包含 `scripts/apparmor/bwrap-userns-restrict` 官方配置，克隆后无需取得本机 artifacts 才能检查或安装。只有本机存在对应验收运行目录和已登录管理员配置时，才使用 `--retest` 更新该次运行结果；其他机器安装后运行 `python3 scripts/codex-sandbox-preflight.py` 验证原生限制。
