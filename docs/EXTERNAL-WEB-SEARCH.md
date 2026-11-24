# 让 Pi 使用分组授权的搜索服务

在管理员面板打开「配置 → 外部搜索」，点击「添加搜索服务」。选择 DeepSeek 并填写官方 API Key，或选择 SearXNG 并填写已启用 JSON 输出的搜索地址。可以给同一服务商添加多条配置。

1. 保存后点击「测试」。DeepSeek 测试会执行一次独立搜索，成功且有结果时由服务商收费。
2. 启用该服务并点击「设为默认」。进入「账号分组」，编辑客户端密钥所属的分组，勾选该搜索服务并保存。
3. 在 Pi 所在机器的项目目录执行安装命令，然后重启 Pi 或执行 `/reload`。选择通过网关配置的 `codearts-local` 或 `codearts` 模型。

```sh
python3 scripts/connect-client.py pi --install-web-search
```

新电脑也需要安装一次项目提供的 Pi 扩展。如果这台电脑只有 Pi，可以直接把同版本的 `scripts/pi/codearts-web-search.ts` 复制到 Pi 配置目录的 `extensions/` 下，默认位置是 `~/.pi/agent/extensions/codearts-web-search.ts`。启动 Pi 或执行 `/reload` 后，搜索结果默认只显示条数。按 `Ctrl+O` 展开标题、链接与摘要，再按一次收起。

在 Pi 输入「请用 web_search 搜索 arXiv 上的 Attention Is All You Need，并给出来源」。工具返回标题、来源链接和摘要。服务商密钥由网关持有，Pi 沿用模型已有的网关密钥。卸载项目扩展可运行 `python3 scripts/connect-client.py pi --remove-web-search`。自定义 Pi 配置目录可用 `--pi-models-file /path/to/models.json` 指定。

其他客户端可以直接发送以下请求。用部署地址替换本地地址。省略 `service_id` 使用默认服务，显式选择其他配置时填写该服务的 ID。

```sh
curl http://127.0.0.1:10087/v1/search \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"query":"Attention Is All You Need site:arxiv.org","max_results":3}'
```

管理员承担外部搜索费用。搜索占用用户请求次数和并发额度。收到 403 时检查密钥绑定和分组授权，默认服务缺失时由管理员重新指定。删除服务会同时移除对应授权。模型渠道原生搜索继续使用各自的模型协议和权限。

## 升级已有实例

schema 11 升级到 12 前停止写入并备份完整数据目录。受管安装使用候选发布包的 `panelctl upgrade --migrate-schema` 流程。迁移后外部搜索配置和授权从空列表开始。先在隔离实例验收，再切换实际服务。原有平台搜索设置仍从原入口读取。
