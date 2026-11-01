# 控制台前端

Node 24.11.x、pnpm 10.34.9；准确依赖见 package.json 和 pnpm-lock.yaml。生产只需要 Go 二进制，不需要 Node 服务。

从仓库根执行：

```sh
./scripts/frontend.sh install --frozen-lockfile
./scripts/frontend.sh check
./scripts/frontend.sh build
python3 tools/check_api_types.py
```

`make build` / `build.sh` 自动构建前端后编译 Go。直接运行 Go 命令前先构建一次前端，缺少产物时编译失败。`frontend/dist` 和 `backend/internal/webui/dist` 都不提交。

- `src/app`：装配、会话、导航；不存平台凭据。
- `src/api/client.ts`：同源请求、Cookie/本机能力头、统一错误、查询取消。`generated/admin.ts` 从 `api/admin.openapi.yaml` 生成，不手改。
- `src/features`：八个页面、弹窗、功能状态及控制器；账号内的 `providers` 保存平台专属授权表单与流程，WorkBuddy 成长交互归 `jobs`。
- `src/shared/read-model.ts`：服务端公开快照；不和表单草稿混用。
- `src/components/controls.ts`：严格类型的安全文本、表格和按钮助手，包含重复提交保护。
- `src/layouts`、`src/styles`：原有导航和浅黄色明暗主题。

这次保留成熟的原生 DOM 渲染算法，将其按功能拆成 ES 模块；这些控制器仍为 JavaScript，`checkJs: false`，不能将本次类型检查理解为所有遗留函数已经严格类型化。新增 API 客户端、公共控件、装配、SFC 脚本及契约使用 TypeScript。相关限制见 [M4 验收](../docs/acceptance/STRUCTURE-M4-COMPLETE-2026-11-02.md)。

每个静态 Vue 页面根使用 `v-once`，组件持续挂载，页签只切可见性，以保留草稿。原生控制器独占其内部 DOM，Vue 不绑定这些节点的响应式子树。旧 console.js 已删除。新增响应式交互必须拥有独立组件根，同时迁出对应原生事件，不能让两种渲染器共同更新一个节点。

跨功能只调用显式导出的函数，不引用其他 feature 的状态对象。应用卸载清除事件和轮询、取消查询；切页只取消可替换的页面查询，已提交写操作不因此取消。成长任务原有关闭取消和未知结果规则不变。默认查询不重试，推理接口不经过此前端客户端。

`make dev` 启动 Vite 5173 和临时后端 10089，使用临时数据，不读取运行账号。首次管理员凭据位于该临时目录的访问文件中；终端显示目录。按 Ctrl+C 停止两个进程组。代理转发 `/admin`、`/v1`、`/health`、`/metrics`，只转换合法开发 Origin，标记代理来源，不能借用本机自动解锁。生产 CSP 保持不变。

浏览器验收：先构建二进制；本机默认使用 Chrome，CI 使用锁定的 Playwright Chromium。

```sh
./scripts/frontend.sh exec playwright install chromium
CODEARTS_TEST_BROWSER=chromium make e2e
```

测试只使用临时数据和模拟接口，不会真实登录、领取奖励或消耗积分。Linux 上已实测开发代理和退出；Windows CI 构建和运行后端测试，Windows 可分别在两个终端启动 Vite 与 Go。
