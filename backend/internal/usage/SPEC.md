# 价格管理边界

M3.19 仅迁入 ModelPrice 的规范值、原 Validate 算法和管理协调。Store.ModelPrice 是兼容类型别名，JSON 字段及 schema 6 不变；RequestCost、TokenUsage、估算、历史快照、用量账本、预算和保存事务仍在原 Store，后续 M5 独立迁移。

Management 借用 PricingStore，不缓存价格。HTTP 保留 8KiB 宽松解码、型号修剪、模型/三项单价必填校验；业务保持先价格合法性、后型号长度、最后严格保存的顺序。USD、0 到 1000000、非有限数拒绝、来源长度均为原规则；显式 0 与未配置必须区别。

严格保存仍由 SetPriceAndSave 完成：原时间戳/默认来源、保存锁、内存发布、写盘与失败回滚不变。ErrPriceSave 只供 HTTP 映射原 500，保留底层错误链；验证失败仍为原 400。此服务不重算历史、不更新上游积分单价、不创建价格版本缓存，不介入普通推理或账本事务。
