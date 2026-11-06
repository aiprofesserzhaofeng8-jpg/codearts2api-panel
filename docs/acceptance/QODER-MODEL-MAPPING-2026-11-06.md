# Qoder CN 共享型号映射验收

2026-11-06，用户明确确认 dfmodel 是 V4.1 Flash，并授权将已测试 Qoder 型号加入模型映射。

默认目录升级到版本 3，新增十项 CN 专用映射。dfmodel 的精确版本以用户确认为依据，其他项依据当日 Qoder 官方账户目录显示名。fast/auto 不合并。新建绑定同时核对内部代号与显示名，不把 CN 证据套用到 global 账号。保留旧 qoder-* 定义用于兼容调用，列表在标准模型覆盖对应来源时隐藏重复入口。

验证：完整 make check（56 项前端、100 项部署、完整 Go/vet/架构及类型检查）通过；映射、目录变更、地区隔离、旧名称兼容和共享模型列表回归通过定向 race。隔离副本确认十项映射全部加入且 fast 独立。

本机系统服务已升级为 v0.4.3，并在停写备份后原子应用十项映射。deepseek-v4.1-flash、glm-5.3、glm-5.3-flash、kimi-k3.1-pro 各包含四个平台来源；完整结果见忽略目录 artifacts/local-update-v0.4.3/deployment-receipt.json。账号、密钥、用户、分组和 settings 逐字段对比保持。运行服务 health 正常，7 个账号全部就绪。

备份：/opt/codearts2api/backups/before-qoder-mapping-1793963112640088260.tar.gz。

升级后真实 Pi 使用共享名 deepseek-v4.1-flash 返回 QODER_MAPPING_OK，退出码 0。该调用证明共享入口仍可用，不据此声称本次随机选中了 Qoder。

已知限制：Qoder 官方目录的 max_output 为 0（未知），新来源加入后 DeepSeek 公共 client-profile 返回 422（资料不足）；已有 Pi 配置可继续调用。没有为通过 profile 校验而编造输出上限，后续应单独确认该参数。工具能力也不由文本成功推断。mmodel 前次上游502不因加入映射而标记恢复。

本批仅更新本机，未创建 GitHub Release。真实目录与11型号文本结果见 artifacts/qoder-alias-live/REPORT.md；本批补充一条 Pi 共享名短文本验证。
