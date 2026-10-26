# 贡献与提交说明

## 修改说明写在哪里

README 描述当前产品功能、安装、使用方法和稳定入口。逐次新增、修复、迁移、测试结果和未解决问题写入对应 Git commit 的正文；需要集中发布说明时，再从这些提交整理到 GitHub Release。

每次提交应包含：

1. **原因**：什么场景存在问题，或为什么需要该能力。
2. **改动**：用户可观察的最终行为，以及关键实现选择。
3. **验证**：实际执行的检查和结果；未执行的测试要说明。
4. **影响与限制**：是否需要重启、迁移或客户端操作，以及仍未解决的事项。

小改动可精简，但不能将“未测试”写成“测试通过”。涉及真实上游、并发或权限的修改，应记录相应证据范围。

建议标题形式：`feat: ...`、`fix: ...`、`docs: ...`、`test: ...`、`chore: ...`。

```text
fix: 修复大段历史的摘要恢复

原因：批量回填历史后，客户端将摘要输出预算压得过小。
改动：增加有调用次数上限的分段摘要，所有分段完整结束才提交检查点。
验证：模拟故障测试通过，原失败场景完成三轮真实复测。
影响与限制：运行中的 Pi 需要重新载入扩展；超过恢复上限仍须缩减历史。

Assisted-by: OpenAI ChatGPT / Codex
```

`.gitmessage` 提供提交模板。在当前克隆中启用：

```bash
git config --local commit.template .gitmessage
```

AI 尾注仅在实际使用相关工具辅助时添加；贡献说明见 [CONTRIBUTORS.md](CONTRIBUTORS.md)。无需重写已发布的历史提交。

## GitHub 提交身份

GitHub 根据提交作者邮箱关联账号；仓库所有者、推送者与提交作者可能不同。提交前检查本机身份，必要时仅为当前仓库设置自己的 GitHub 用户名及已关联邮箱：

```bash
git config --local user.name "你的 GitHub 用户名"
git config --local user.email "GitHub Settings → Emails 中提供的提交邮箱"
git var GIT_AUTHOR_IDENT
```

保留 README 中的 AI 工具署名和 `Assisted-by` 尾注。它们是辅助开发记录，不用于模拟 OpenAI 官方账号提交。GitHub 右侧贡献者统计由平台生成；不要为改变名单创建空提交或冒用其他账号的邮箱。

## 验证

按修改范围执行检查：Go 逻辑使用 `go test ./...`、必要时 `go test -race ./...` 和 `go vet ./...`；客户端配置使用 Python 单元测试；Pi 恢复使用 `node scripts/pi/codearts-compaction.test.mjs`。纯文档修改检查命令、链接和说明与当前实现是否一致。

完整验收流程见 [质量验收方案](docs/acceptance/QUALITY-PLAN.md)。提交不得包含运行账号、访问凭据、密钥、私有会话或原始测试数据。
