# v0.4.0：全局统一模型与受管升级

本版本包含全局统一模型 U1–U6，以及 WorkBuddy/Trae 真实验证后补齐的工具能力标记。状态格式从 schema 10 升至 11；旧程序拒绝读取新状态。发布包与线上实例分开核对，构建通过不代表服务已更新。

## 功能

- 在模型页一次维护公开名称、来源、启停及客户端参数，再给分组分配账号、来源和独立的可见/可调用权限。
- 不分组密钥可以识别全局模型；Chat Completions、Responses、Messages、模型列表和 client-profile 使用一致解析。
- 暂时缺失的来源保留配置；版本冲突保留草稿；旧分组映射编辑入口撤下。
- WorkBuddy 保留上游 supportsToolCall 的 true/false/缺失状态；Trae 仅为已完成真实工具闭环的精确 deepseek-v4.1-flash 配置补充支持。
- 安装脚本支持显式 upgrade --migrate-schema；恢复工具提供只读检查及 recover-schema --apply 向前继续中断的升级。

## 从 v0.3.18 升级

使用固定标签脚本，避免 main 分支缓存仍返回旧参数集：

```bash
curl -fsSL https://raw.githubusercontent.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/v0.4.0/deploy/install.sh | sudo bash -s -- upgrade --version v0.4.0 --migrate-schema
```

脚本验证版本、附件和摘要；候选程序先做只读预检查，停写后再次检查并完整备份数据，再迁移到新目录、切换程序和验证实际进程。账号、密钥、预算与账本保留；同名但来源不同等冲突会在停机前拒绝自动合并。

已经安装同一版本时会明确提示“发布源没有更新版本”，不会停止服务或改写数据。它不会把重复安装当作升级，也不会自动降级。

升级后检查：

```bash
sudo /opt/codearts2api/current/codearts2api-panel -version
sudo systemctl status codearts2api.service
curl -fsS http://127.0.0.1:10087/health
```

预期程序版本为 codearts2api v0.4.0。自定义安装目录、端口或用户级服务需替换为对应值。初始化已完成的实例不重新安装，也不重置管理员密码。

## 恢复

如果出现 schema-migration.json，保留所有目录与记录，使用新包中的 panelctl.py 检查：

```bash
sudo python3 panelctl.py recover-schema --root /opt/codearts2api
```

默认仅检查。确认继续记录指定的新版本后，使用 --apply；候选尚未发布到安装目录时须同时提供同一候选包及摘要。完整说明见 [统一模型升级与恢复](../../deploy/UNIFIED-MODELS.md)。恢复始终保留最新状态向前继续，不自动用历史快照覆盖已轮转凭据或新账本。

## 验证范围

- U1–U6：完整 Go、前端、部署、协议/权限/计量、浏览器、Pi 模拟上游、安装包及隔离升级恢复检查，见 [U6 验收](UNIFIED-MODELS-U6-2026-11-05.md)。
- 真实上游：WorkBuddy CN 与 Trae CN 的 deepseek-v4.1-flash，三协议流式/非流式 12 项通过；原始与统一名称工具闭环修复后 4 组通过，见 [真实验证](UNIFIED-MODELS-LIVE-2026-11-05.md)。
- 本轮没有验证所有平台/型号、图片/音频或长上下文极限；不同平台所宣称的型号身份不能通过回答内容独立证明。
- 发布工作流分别执行 Windows 原生包验收、Linux/systemd/Docker 检查和 schema 10→11 的 7 个中断布局向前恢复。最终发布结果以对应 GitHub Actions 运行与 Release 附件为准。

此前 U1–U6 文档和机器记录中的“未发布/未部署”是各阶段记录时的状态。本版本发布不会自动更换任何用户正在运行的服务；需显式执行上述升级命令。
