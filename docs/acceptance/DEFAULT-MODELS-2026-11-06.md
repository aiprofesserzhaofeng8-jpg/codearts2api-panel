# 默认模型目录验收（2026-11-06）

## 目标与行为

管理员不再逐个平台填写同型号映射。在“模型 → 默认模型目录”预览当前八个平台目录的公开名称与实际来源，搜索、平台筛选、全选后一次应用。后续刷新模型清单，再应用可追加来源。

- 内置版本 1 对照表明确匹配已知平台/型号；DeepSeek V4.1 Flash 合并 CodeArts、WorkBuddy、Trae 对应来源。其他平台专用型号保留平台前缀，不按显示名猜测等价关系。
- 每个已获得的原始型号进入默认目录；未知型号不填造能力或上下文。没有接入/没有目录的平台不生成假来源。仍沿用每定义 1–16 个来源和每平台地区一个型号的约束。
- 可以使用原始型号名作为公开名，也允许 qoder-auto 等无数字产品名。完整原始 ID 保留直调。已有 legacy 分组无统一授权时保留原始裸名；显式禁止的统一授权不能回退绕过。
- 应用复用已有定义及旧 deepseek-4.1-flash 名称，保留 ID、策略、停用来源与手动绑定。自定义别名键冲突明确显示；已有原始别名目标仍可继续保存设置。
- 可选同时授权到一个分组，仅为未授权模型追加账号及原始 ACL 允许的固定来源。已有授权及限制不改变。配置和授权同一事务提交；重复应用无变化不改 revision。
- 目录摘要/预设版本/路由及分组 revision 防止旧预览覆盖新配置。批次错误与磁盘失败全部回滚。
- 手动创建重复名称时提供“编辑已有模型并添加来源”。中英文文案、桌面/手机布局已补齐。

## 实现边界

版本化对照表在 backend/internal/catalog/default_models.json；catalog 只生成 Registry 投影及建议，config 定义请求值，storage 在原 saveMu/mu 中一次持久化。HTTP 使用已有管理员会话与写保护。前端 DefaultModelsPanel 独占响应式节点，既有原生目录节点保持 v-once。

GET/POST /admin/api/model-defaults 已加入 OpenAPI 和生成类型。无需新增第三方依赖、运行时网络查询、另一个网关或凭据副本。普通列表及推理仍使用已有统一模型解析/授权/计量。

状态字段布局仍为 schema 11；旧二进制可能拒绝新的合法名称，应用后不支持退回旧程序。旧 schema 10→11 离线迁移继续使用严格名称冲突规则。

## 验证

- make check：55 项前端测试、100 项部署测试、完整 Go 测试、vet、API 类型同步、JS/CSS 和架构依赖检查通过。
- catalog/storage/server 的 TestDefaultModels、TestNativeName、TestUnified 定向 race 通过；最终补充的旧别名目标兼容用例也单独通过 race。
- 默认目录用例覆盖八个平台模拟目录、已知来源归并、同名原始直调、幂等应用、状态重开、失效预览、未认证写入、分组 ACL、旧定义保留、已有禁止授权、分组版本冲突、批次/磁盘失败回滚。
- 真实应用装配 + 模拟平台 + Chrome 浏览器验收通过：默认表预览、批量应用、刷新保留、重复预览、已有编辑/分组/CAS/策略回归和桌面/390px 手机布局。
- 浏览器截图和机器记录位于忽略目录 artifacts/default-models-browser/；日志位于 /tmp/codearts-defaults-final-check.log、/tmp/codearts-defaults-race.log、/tmp/codearts-defaults-final-browser.log。

## 交付状态

这是本地源码验收，尚未提交发布或替换既有运行服务。没有使用真实凭据、执行真实推理/领奖/续期或修改现有账号数据。模拟目录测试证明处理所有平台的配置路径，不等同于逐个真实上游模型的生成能力验证。

## 本机更新（用户随后授权）

本地构建 v0.4.1 已通过 14 项隔离包升级/恢复检查，并使用现有 panelctl 更新 /opt/codearts2api 的 codearts2api.service。系统认证后先停写备份，再切换并启动；程序返回 codearts2api v0.4.1，服务 active。模型目录恢复后 health 为 7 个账号、7 个就绪、115 个型号；HTTP 实际加载的新前端含默认目录入口。

备份：/opt/codearts2api/backups/before-v0.4.1-1793897751710073550.tar.gz。没有自动应用默认表或修改分组授权。机器回执在忽略目录 artifacts/local-update-v0.4.1/deployment-receipt.json。当前仅为本机源码构建更新，尚未创建 GitHub v0.4.1 Release。
