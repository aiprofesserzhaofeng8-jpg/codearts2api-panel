# 全局统一模型 U2 验收记录

日期：2026-11-05。状态：U2 已完成。范围：全局定义、原子保存与 schema 10→11 迁移。真实运行数据未迁移；公开 API 与路由切换分别属于 U4 和 U3。

## 交付

| 交付 | 文件 | 行为 |
| --- | --- | --- |
| 全局定义及严格校验 | [config/unified_models.go](../../backend/internal/config/unified_models.go) | 稳定 ID、版本化名称、来源数量/平台区域唯一、策略边界与缺失/null 拒绝 |
| 组授权纯值 | [access/unified_models.go](../../backend/internal/access/unified_models.go) | selected/all、独立 visible/callable、空集不放权、深拷贝 |
| 管理协调 | [catalog/management.go](../../backend/internal/catalog/management.go) | 实际目录归一、拒绝未知新来源、保留缺失旧来源；app 装配，HTTP 端点留 U4 |
| 原子 CRUD | [storage/unified_models.go](../../backend/internal/storage/unified_models.go) | 模型/策略/routing revision 同事务、CAS、引用删除保护、落盘失败回滚 |
| 相关配置事务 | [storage/model_settings.go](../../backend/internal/storage/model_settings.go) | 原始策略及模型设置版本条件；强制模型/别名冲突拒绝 |
| 迁移核心与报告 | [storage/model_migration.go](../../backend/internal/storage/model_migration.go) | 确定性 ID、同名冲突、固定旧授权、重跑幂等、脱敏只读报告 |
| 离线完整目录迁移 | [storage/model_migration_files.go](../../backend/internal/storage/model_migration_files.go) | 新目录、全部普通文件、0600/0700、链接/覆盖/重叠/活跃写者拒绝、journal 重放 |
| CLI | [main.go](../../backend/cmd/panel/main.go) | 在初始化/平台装配前执行只读预检查或离线转换并退出 |
| 受管升级 | [panelctl.py](../../deploy/panelctl.py) | 显式 --migrate-schema、包声明、停写前后预检查、备份、隔离迁移、恢复标记 |
| 包元信息 | [package_release.py](../../deploy/package_release.py) | schema 11 Linux 包声明支持 from=10/to=11；不放开任意 schema |

## 阶段兼容

磁盘 schema 11 只保存 `unified_models` 和各组 grants，不再保存 model_mappings。U3/U4 尚未实施，为保持现有功能可运行，`Groups()/AccessScope()` 暂时从全局定义和 grants 生成旧 ModelMappings/ModelRules 视图，现有组编辑的 `SaveGroup` 也通过一次转换写入唯一全局状态。

旧适配入口保持原有写入语义，不要求 HTTP 的 expected_revision；新的 `SaveUnifiedGroup/DeleteUnifiedGroup` 和统一 CRUD 强制 CAS。U4 将 HTTP 切到新的严格接口并删除旧映射写入口。不得把临时兼容接口称为已经实现 U4。旧转换写入只能引用已有定义的来源子集；不能通过组修改扩大另一组的来源。

现有跨来源执行、响应改名和账号健康仍走旧逻辑；不分组密钥的全局模型解析、新能力筛选及统一 profile 仍待 U3/U4。本阶段没有声称 G1–G7 整体已完成。

## 自动化覆盖

- 新增 16 个 Go 顶层用例（包含多组子用例），覆盖 CRUD 往返、快照隔离、双写 CAS、无效/冲突名称、删除引用、空/all 来源、缺字段拒绝、revision 耗尽、策略边界、设置保存回滚。
- 迁移覆盖同名同源合并、顺序差异、同名异源/大小写/裸名称/内置/自定义别名/强制型号/默认值冲突、无账号保留、损坏/未知字段/未来 schema 拒绝、输出目录与符号链接拒绝。
- 使用现有账本夹具验证未 checkpoint 记录在转换副本只重放一次，预算/费用/记录与原运行状态一致，再次打开不重复计费；输入主状态不改动。
- 新增 5 个部署用例：停机前冲突、两次预检查、保存原目录、启动后故障保留新凭据、未完成标记阻止下一次升级、输出不泄露秘密。
- 旧 `summary_target_test.go` 中“运行时强行写入 ForceModel”不再是合法状态，其冲突拒绝测试迁至 storage；其他替换授权/冷却/预算测试保留。

## 实际 CLI 验收

使用本次构建的 `/tmp/codearts-u2-panel` 和已下载的 v0.3.18 schema 10 二进制，在临时目录执行 [check_unified_migration.py](../../tools/check_unified_migration.py)：

```bash
python3 tools/check_unified_migration.py \
  --binary /tmp/codearts-u2-panel \
  --old-binary artifacts/releases/v0.3.18-verified/verified-panel \
  --output artifacts/unified-models-u2-cli.json
```

7 项通过：只读预检查、新目录完整迁移与权限、schema 11 检查幂等、已存在输出拒绝、冲突退出码 2、真实活跃进程检测、旧二进制拒绝 schema 11。输入为虚构账号/来源，没有生产密钥或上游请求。

新命令（执行前需选择已停写副本，不要直接套用到运行目录）：

```bash
/path/to/new-panel -check-state-migration /path/to/copy/state.json
/path/to/new-panel -migrate-state-dir /path/to/stopped-copy \
  -migration-output-dir /path/to/new-directory
```

Linux 受管升级通过新包内的 `panelctl.py upgrade ... --migrate-schema` 启用。旧 install.sh 未增加隐式跨 schema 放行；未指定迁移参数仍在停机前拒绝。

## 验证结果

- `make check`：首轮及最终复核均通过（前端 45 项、Go 全包、vet、架构/生成类型检查、Node helper 和部署 91 项）；机器证据见 [U2 检查记录](UNIFIED-MODELS-U2-CHECKS-2026-11-05.json)。
- storage/catalog/config/access/app 全包 race 通过；最终新增检查的 U2 定向 race 也通过。
- Windows `GOOS=windows go vet ./...` 通过。离线迁移的活跃写者检测目前仅 Linux；Windows 二进制可编译，不宣称 Windows 进程检测或受管升级已实测。
- 所有 Go 命令使用固定 go1.27.3，`GOMAXPROCS=2 GOFLAGS=-p=2`；直接 Go 前已构建前端。
- `scripts/connect-client.py` 原有未提交差异保持，SHA-256 仍为 `94ca9a14900792421df6a12f6226b8e8e3a082c0b8dd5370fb3c18c170c11100`。

## 限制与恢复

离线报告会声明没有联网验证目录/账号可用性。它验证配置结构与权限，不等于真实同型号跨平台调用可用。

受管迁移保存原数据目录、备份和阶段记录；切换开始后失败会停止候选，不自动恢复旧凭据。`schema-migration.json` 尚在时再次 upgrade 拒绝，候选只在指定 starting 阶段与指定程序路径匹配时允许启动。U6 再验收真实 systemd/完整发布包及各崩溃点，不能用本阶段 mock 服务测试替代。

未执行生产数据迁移、服务替换、GitHub 提交或发布。下一阶段 U3 负责全局解析、准入和同型号切换。
