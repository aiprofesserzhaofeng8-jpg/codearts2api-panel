# 模型参数参考表

`models.dev.json` 是网关离线使用的公开模型规格表，保留 models.dev 的提供方、精确模型 ID、名称、上下文和最大输出字段。当前快照包含 18 个提供方、523 条记录；记录数包含同一模型在不同提供方的重复条目，不代表网关可用模型数。来源、获取时间和版本见 `snapshot.json`，数据授权见 `LICENSE`。

该表复用 Sub2API 所使用的 models.dev 数据源和现有解析器。它不包含计费价格；项目现有计费逻辑保持独立，也不把原生图片、工具能力自动授权给反代渠道。

## 更新

在项目根目录执行：

```sh
python3 scripts/update-model-reference.py
```

命令按 `client_references.go` 中现有提供方清单抽取数值，不改实例策略、本机 Pi 或实测报告。下载失败不会覆盖本地表。更新后审查差异，并运行目录测试；新快照随下次构建发布。运行中网关仍沿用既有在线同步和目录持久化流程，不需要为日常在线更新重新编译。

如果 models.dev API 无法访问，可使用该项目自己的生成器生成相同格式的数据，不手工解析其 TOML 继承关系。本次环境 API 返回 HTTP 403，采用以下路径：

```sh
git clone --depth 1 https://github.com/anomalyco/models.dev.git /tmp/models-dev
cd /tmp/models-dev
bun install --frozen-lockfile --ignore-scripts
bun -e 'import {generate} from "./packages/core/src/generate.ts"; await Bun.write("/tmp/models-dev-api.json", JSON.stringify(await generate(process.cwd()+"/providers")));'
git rev-parse HEAD
```

回到本项目根目录，将上述提交号填入参数：

```sh
python3 scripts/update-model-reference.py --source /tmp/models-dev-api.json --source-revision <提交号>
```

## 使用顺序

1. 已保存的人工模型策略保持最高优先级。
2. 自动参考沿用在线精确匹配；在线不可用或无对应记录时，保留同一身份的已保存参考。
3. 没有已保存参考时，按既有精确名称和已审核别名查询本表。
4. 本表也无对应项时，继续使用现有上游目录和兼容默认建议。

本表只补充已经发现的模型，不新增路由或可用模型。槽位身份变化时不沿用旧快照或离线参考；未知自定义槽位不根据 `1M` 等名字猜测参数。统一模型继续按授权来源计算建议。

渠道实测记录仍保存在 `docs/acceptance/`，Pi 工作参数仍由 `scripts/pi/model-settings.json` 管理。公开规格、人工工作配置和实测成功输入量分别维护。
