# 管理 API 契约

`admin.openapi.yaml` 是 OpenAPI 3.1 文档，采用 JSON 语法（属于 YAML 1.2 子集），供 Go 标准库校验和前端类型生成共同使用。接口清单覆盖当前管理路由；State、账号、分组、密钥、模型、登录状态和错误等公共 DTO 有明确字段，平台专有操作的可变载荷保持开放。

修改公共 DTO 时同时更新接口、此契约及消费它的页面，在仓库根目录执行：

```bash
./scripts/frontend.sh generate
python3 tools/check_api_types.py
make check
```

生成文件为 `frontend/src/api/generated/admin.ts`，不手工编辑。Go 契约测试核对管理路由清单与选定运行响应；它不代替各业务接口的行为测试，也不在推理路径过滤或重写 JSON。

保持原接口的兼容语义：例如分组 `model_mappings` 缺省或 `null` 保留现值，`[]` 显式清空。价格、预算和 Token 单位遵循字段说明；类型声明不改变后端授权、校验和计量规则。

M7 在同一契约维护 `/admin/api/users`、`/user/auth/*` 和 `/user/api/*`。User DTO 不包含密码摘要；普通用户键与用量采用显式安全投影，用户身份固定来自会话，客户端不能提交所有者、分组或预算。契约类型不等于授权，服务端仍逐次核验身份及资源归属。
