**版本定版 · 2026-10-30 02:00（Asia/Shanghai）**

版本标签：`snapshot-20261030-020016-verified-tools`。

本次仅归档已有验收文件并建立回退点，没有修改业务代码、默认提示词、账号设置或调度策略，也没有重建、替换或重启正在运行的服务。

**版本身份与备份**

- 业务源码基线：`b6891bfe3e93ab492763b6ffdbdf1d0a131302d7`，包含 Messages 流式多工具隔离修复。
- 运行程序与 `bin/codearts2api-panel` 的 SHA256 均为 `3cd383d560b7c966bb42c8f9ce171560bdc738f7ecd6adad4474b3824d17c0aa`。
- 私有备份：`data/backups/version-freeze-20261030-020016/`，包含运行程序、源码归档、Git bundle、状态与访问配置、服务单元、校验清单和恢复说明。状态与访问配置仅保存在本机被 Git 忽略的目录，目录权限为 0700，数据文件为 0600。
- 定版前已有的 27 份未提交验收文件一并归档到本次提交；备份中的 `pending-acceptance-originals.tar.gz` 和 `manifest.json` 另保留其原始字节及 SHA256。原始测试日志中的空白字符不做格式化改写。

**已验证的范围**

- 三平台 54 个真实工具闭环全部返回正确工具参数与结果值；严格最终格式通过 50/54。27 条真实 Messages 流经官方 Anthropic SDK 离线解析，与独立解析器一致。
- 既有完整竞态测试、vet、浏览器设置与布局验收、性能微基准见 [Messages 修复验收](MESSAGES-TOOLS-FIX-2026-10-30.md)。本次定版未重复调用真实上游，也不将此前的模型样本扩展成所有模型的保证。
- 定版时重新运行非 JSON HTTP 链路与 Messages 工具相关定向竞态测试，通过；日志见 [targeted-tests.log](evidence/version-freeze-2026-10-30/targeted-tests.log)。
- 定版前健康检查 HTTP 200，账号数为 12，在途请求为 0；设置摘要和运行程序哈希记于 [verification.json](evidence/version-freeze-2026-10-30/verification.json)。这些是采样时状态，不是持续可用率。

**保留的边界与后续工作**

- WorkBuddy 的 4 例偏差仅为最终答案多一层 Markdown 代码块，原始值正确；按用户决定保留当前行为。
- Trae 曾偶发返回过多独立工具调用，本批未复现，不能宣称已经修复。
- 响应结构补充校验、Messages thinking 参数映射、质量计时、异常退出后的用量持久化、取消与超时恢复、客户端和更高负载验收仍分别评估。后续优先验收取消与超时，有实际问题再作小范围修复。

**恢复方式**

只恢复程序时，应使用本目录备份中的 `codearts2api-panel` 替换项目 `bin/codearts2api-panel`，随后启动原服务；操作前先停止服务并确认在途请求已结束。保留当前 `data/state.json` 与访问配置，使后续刷新得到的凭据、用量和预算继续生效。

需要检查冻结源码时，可在独立工作目录检出上述标签，或解压私有备份中的 `source.tar.gz`。`repository.bundle` 可用于恢复本机 Git 丢失后的源码历史。恢复状态文件属于额外的数据恢复操作，会回到备份时刻，不应与普通程序回退混为一项操作。
