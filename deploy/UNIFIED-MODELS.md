# 全局统一模型：使用、升级与恢复

适用 schema 11。公开模型名由管理员确认对应关系后配置，不根据名称猜测真实模型身份。该说明随 Linux 安装包提供；本地验收包和 GitHub 正式发布是独立状态。

## 管理员使用

1. 登录管理面板，在账号池导入账号、刷新各平台目录。记录各自准确的完整型号。
2. 打开“模型 → 全局统一模型”，创建带版本的公开名，例如 `shared-model-1`。从目录选两个已确认属于同一型号的来源。公开名创建后不能改名；换版本应创建新定义。
3. 客户端策略缺省按所有启用且有权来源取保守建议。任一来源缺上下文/输出资料时，先补齐原始模型策略；不要猜测参数。一次文本测活不能证明图片或工具能力。
4. 不分组的管理员密钥可识别全局定义。分组密钥、普通用户还需在“账号分组”选择该模型、账号和来源；可见与可调用是独立权限。固定来源不会自动包含未来新增来源；明确选择“全部”才跟随新增来源。固定空集表示禁止。
5. 默认模型处理未填写型号的请求；客户端偏好仅用于推荐参数。完整原始型号仍有独立权限校验。旧分组映射编辑入口已撤下。
6. 来源从目录暂时消失时保留配置并显示缺失，不自动替换或删除。保存冲突保留草稿，明确重新读取后核对；不会覆盖别的管理员的新版本。

工具请求需要来源具有明确的工具能力。WorkBuddy 从上游 `supportsToolCall` 读取；Trae 目前仅为完成真实工具闭环验证的精确型号 `deepseek-v4.1-flash` 补充标记，其他型号保持未知。v0.4.0 包含这些标记，旧程序或尚未刷新的目录不能视为已经包含。

调用接口使用同一个公开名：`/v1/chat/completions`、`/v1/responses`、`/v1/messages`。`GET /v1/models` 返回授权目录，`GET /v1/client-profile?model=shared-model-1` 返回参数；两者均需网关 API key。列表可见不保证账号此刻有额度。

Pi 使用既有启动器；下面命令会提示输入网关密钥：

```bash
python3 scripts/connect-client.py pi --url http://127.0.0.1:10087 --model shared-model-1
```

启动器通过进程环境传递密钥，不把密钥字面量写入模型配置。它不提供客户端自动识别隐藏型号的能力。

## 10 → 11 升级

先下载并验证候选包和校验值。以下路径、版本及摘要必须替换为本机实际值，不能直接运行示例占位符。使用候选包里的 `panelctl.py`，旧工具没有迁移和恢复命令。

先用候选程序做只读预检查：

```bash
./codearts2api-panel -check-state-migration /opt/codearts2api/data/state.json
```

返回 0 表示可迁移或已为目标版本，2 表示配置冲突，1 表示文件错误。报告不输出凭据。相同名称指向不同来源、大小写冲突、默认引用冲突、强制模型等须在旧配置中显式处理后重试。离线报告没有验证真实上游型号。

受管升级必须显式指定：

```bash
sudo python3 panelctl.py upgrade --root /opt/codearts2api \
  --archive /path/codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz \
  --sha256 SHA256_FROM_VERIFIED_RELEASE --migrate-schema
```

工具在停服务前预检查，停写后再次检查并完整备份，再迁移到新目录、切换、启动和核对真实进程。无此参数的跨 schema 升级会拒绝。Docker/源码部署也必须单实例停写，备份完整目录和账本，使用 `-migrate-state-dir INPUT -migration-output-dir NEW` 生成副本后再切换；不能两个进程共写，不能只手工改 version。

v0.4.0 的一行安装入口支持显式参数。使用固定标签可以避免 main 缓存仍返回旧安装脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/v0.4.0/deploy/install.sh | sudo bash -s -- upgrade --version v0.4.0 --migrate-schema
```

schema 11 不能交给 schema 10 程序。开始新版本服务后，凭据可能轮转、账本可能新增。失败时应保留最新数据向前修复，禁止自动用旧备份覆盖。

## 升级中断恢复

如果受管根目录存在 `schema-migration.json`，先检查服务和磁盘，保留该记录及所有目录。默认恢复命令只显示阶段，不改变服务：

```bash
sudo python3 panelctl.py recover-schema --root /opt/codearts2api
```

确认继续使用记录指定的新版本后显式执行：

```bash
sudo python3 panelctl.py recover-schema --root /opt/codearts2api --apply \
  --archive /path/codearts2api-panel-vX.Y.Z-linux-amd64.tar.gz \
  --sha256 SHA256_FROM_VERIFIED_RELEASE
```

若中断在发布候选程序前，必须提供原候选包；已发布时无需再次提供包。该命令验证路径、状态、候选版本和进程，停写后按记录向前继续；已有 schema 11 数据始终保留，不重新迁移旧副本。启动失败再次停写，保留记录；修复原因后可重复。完成后记录改名为 `schema-migration-complete-*.json`，再次执行为无操作。

目录不完整、重复候选数据、符号链接、未知阶段或版本不符会拒绝猜测。不要删除标记绕过保护。只在未接受流量、未发生新凭据/账本变化的隔离故障中，才可把完整升级前备份恢复到**新的空目录**进行演练；生产继续服务后的旧快照不能作为自动回退方案。

## 诊断

| 状态 / reason | 含义 |
| --- | --- |
| 404 / model_not_found | 公开名不存在或拼写/大小写不一致 |
| 403 / model_permission_denied | 分组未授权、固定来源为空或原始 ACL 拒绝 |
| 400 / model_capability_unavailable | 没有明确支持图片、工具或输出限制的来源 |
| 422 / client_profile_missing | 来源参数资料不足，模型页补齐资料后重试 |
| 503 / model_sources_unavailable | 模型停用、目录缺失或无可用账号；不代表必须登录某个固定平台 |
| 409 / model_configuration_changed | 请求解析后配置变化，请作为新请求重试 |
| 409 / revision_conflict | 管理草稿版本过期，核对最新版本后重写 |

上游鉴权/限流错误与本地模型权限错误分别记录。首段提交前按原总次数和总期限有限切换；首段提交后不重放。用量按实际平台/型号记账，响应使用公开名。
