---
id: storage
type: module-design
title: storage —— 持久化与原子运行事务
status: implemented
parent: architecture
---

# storage

实现 accounts、access、config、catalog、gateway、jobs、usage 使用方定义的存储接口。此包是持久化状态和运行计数的唯一所有者；不导入 HTTP，不执行上游网络调用。

- `pool.go`：在同一临界区内复核实时权限、选择实际型号、增加 in-flight；纯选择委托 scheduler。黏性、冷却、幂等释放与预算预留不另建缓存或锁域。
- `snapshots.go`：沿用原复制边界；元信息和外部凭据字节复制，CodeArts 不可变代际指针共享。分组、映射、设置别名的可变容器由对应读取方法复制。
- `account_codec.go`、`compatibility.go`：旧 schema/default 与一次解码的 CodeArts 兼容表示；schema 仍为 6，新版本格式拒绝读取。其他平台凭据保持不透明。
- `account_transaction.go`、`session_commit.go`：严格保存与凭据条件提交。轮转 token 落盘失败仍保留新代际，不能恢复旧备份覆盖。
- `files.go`：同一 saveMu 串行快照提交，唯一 0600 临时文件、fsync、原子替换；Linux 同步目录。锁顺序 `saveMu -> journalMu -> mu`。
- `usage_journal.go`：结算同步追加再发布计数；快照检查点后回放、不重复扣费、压缩保留并发后缀。写盘失败保持内存事实并暂停新请求，成功保存可恢复；损坏完整记录拒绝启动，未提交尾部按原规则截去。
- `pricing.go`、`usage_query.go`：持久化和快照适配；计算/过滤/分析委托 usage，预算纯规则委托 access。

严格事务仍在原状态锁内落盘；全局设置与部分历史管理操作仍保留“内存更新、保存失败记日志”的旧语义。日志历史的嵌套值只读，不宣称全量深复制或无锁发布。具体职责见 [ADR 0009](../../../docs/adr/0009-runtime-storage-ownership.md)。

不支持多个进程共写状态。运行中直接复制快照与账本不保证一致；应停机备份或另行实现一致备份。恢复保证限于已提交结算，未确认的上游用量、真实断电、硬盘固件和 Windows 目录同步不在本次验证范围。
