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
- `src/api/client.ts`：同源请求、统一会话 Cookie、统一错误、查询取消。`generated/admin.ts` 从 `api/admin.openapi.yaml` 生成，不手改。
- `src/features`：八个页面、弹窗、功能状态及控制器；账号内的 `providers` 保存平台专属授权表单与流程，WorkBuddy 成长交互归 `jobs`。
- `src/shared/read-model.ts`：服务端公开快照；不和表单草稿混用。
- `src/components/controls.ts`：严格类型的安全文本、表格和按钮助手，包含重复提交保护。
- `src/layouts`：管理员与用户共用 ConsoleLayout、Topbar、品牌、主题开关及 AuthCard；浅黄色明暗主题在 `src/styles`，共用骨架样式独立在 `layout.css`。
- `src/i18n`：语言检测、偏好保存及界面翻译；`locales/en.json` 使用中文界面原文作为键。

这次保留成熟的原生 DOM 渲染算法，将其按功能拆成 ES 模块；这些控制器仍为 JavaScript，`checkJs: false`，不能将本次类型检查理解为所有遗留函数已经严格类型化。新增 API 客户端、公共控件、装配、SFC 脚本及契约使用 TypeScript。相关限制见 [M4 验收](../docs/acceptance/STRUCTURE-M4-COMPLETE-2026-11-02.md)。

每个静态 Vue 页面根使用 `v-once`，组件持续挂载，页签只切可见性，以保留草稿。原生控制器独占其内部 DOM，Vue 不绑定这些节点的响应式子树。旧 console.js 已删除。新增响应式交互必须拥有独立组件根，同时迁出对应原生事件，不能让两种渲染器共同更新一个节点。

跨功能只调用显式导出的函数，不引用其他 feature 的状态对象。应用卸载清除事件和轮询、取消查询；切页只取消可替换的页面查询，已提交写操作不因此取消。成长任务原有关闭取消和未知结果规则不变。默认查询不重试，推理接口不经过此前端客户端。

`make dev` 启动 Vite 5173 和临时后端 10089，使用临时数据，不读取运行账号。打开 Vite 页面完成首次安装，初始化凭证位于终端显示的临时目录 `state.json.setup.json`，用户名固定 `admin`。若显式配置管理密码则跳过向导。按 Ctrl+C 停止两个进程组。代理转发 `/setup/status`、`/setup/install`、`/auth`、`/user/api`、`/admin`、`/v1`、`/health`、`/metrics`，只转换合法开发 Origin，标记代理来源，不能借用本机自动解锁。生产 CSP 保持不变。

浏览器验收：先构建二进制；本机默认使用 Chrome，CI 使用锁定的 Playwright Chromium。

```sh
./scripts/frontend.sh exec playwright install chromium
CODEARTS_TEST_BROWSER=chromium make e2e
```

测试只使用临时数据和模拟接口，不会真实登录、领取奖励或消耗积分。Linux 上已实测开发代理和退出；Windows CI 构建和运行后端测试，Windows 可分别在两个终端启动 Vite 与 Go。

## 界面语言

登录页、首次安装页及管理员/用户顶栏右上角共用 `LocaleSwitcher`，提供“跟随系统 / 中文 / English”。手动选择保存到浏览器 `localStorage.codearts2api_locale`，优先于浏览器语言；未选择或恢复跟随系统时，按 `navigator.languages`、`navigator.language` 检测中文/英文，未识别则默认中文。跟随系统的检测结果不写成手动偏好。

语言在每次加载时确定一次，轮询、切页、其他标签页的存储事件及 `languagechange` 不会改变已挂载页面的语言。参考 Sub2API，手动切换先保存、再刷新整个页面，让 Vue 与原生 `v-once` 控制器使用同一语言；切换前应保存表单草稿。已提交 API 写入期间禁用菜单，避免刷新中断写入的响应处理。本地存储受限时尝试当前标签页的 `sessionStorage`；全部存储不可用或已有只读偏好无法替换时显示错误，不刷新。

新增界面文案显式调用 `t()`（遗留控制器使用 `tr` 别名），并补充英文词条。静态中文原文是中文回退值，不额外维护第二份中文词典。日期和数字使用 `dateLocale`，时区仍跟随浏览器。模型 ID、账号名称、表单值、接口参数和上游内容保持原值；后台错误及活动描述未做服务端国际化。页面关闭浏览器自动翻译，防止第二套翻译器改写界面。

选择规则和组件职责见 [ADR 0016](../docs/adr/0016-interface-language.md)。`i18n-smoke` 使用隔离实例验证检测、持久化、两种角色导航、中文用户数据、手机菜单和存储受限；`setup-smoke` 同时验证安装页的英文检测和中文选择持久化。

M7 的 UsersPage 和 `/user` UserConsole 为独立响应式 Vue 树，API 使用生成类型；不能接管遗留 `v-once` 页面的节点。用户会话只在 Cookie，显式显示的密钥仅留内存，切页/刷新/退出清除。`ConsoleApplication` 统一检查 `/auth/session`；`/login` 登录后按服务端角色进入 `/` 或 `/user`，不提供角色切换入口。原生管理控制器只在管理员身份确认后挂载。开发代理覆盖 `/auth`、`/user/api`。

两端共用侧栏、顶栏和内容区域骨架，通过插槽提供各自导航与操作。管理员导航 Sidebar 及顶栏的标题/退出控件仍由原生控制器独占；`v-once` 只放在它们自己的子树，不放在包含用户响应式分支的条件链上。管理员导航事件只绑定 `#adminNavigation` 内的 `button[data-tab]`，不能匹配整个页面的导航按钮，也不依赖翻译后的标签文本。

用户导航配置归 `features/users/navigation.ts`，使用 `/user#keys` 等片段支持刷新和浏览器前进/后退；切页清除已显示密钥和密码草稿。主题与桌面侧栏折叠偏好共用本机存储，手机导航为抽屉并支持 Escape 和焦点返回。用户页面只请求当前角色的数据，不提供角色切换链接。

统一 API 生命周期由 `ConsoleApplication` 持有，在身份变化时取消旧查询后再挂载对应页面；子页面不能重复配置或销毁共享 API 客户端，避免取消子组件自己的初始请求。

## 全局统一模型编辑器（U5）

`features/models/UnifiedModelsPanel.vue` 和 `features/groups/GroupsPage.vue` 各自独占响应式页面/弹窗。模型页的原始目录、价格和原始策略卡片仍保留 `v-once`，由原控制器管理；分组原生控制器只保留密钥/目录筛选助手，不能再注册组编辑事件。

`shared/unified-models.ts` 只持有编辑器共用的配置读取快照与分页协调，不参与推理权限。分页发现 routing revision 变化则拒绝发布半份清单。表单在打开时捕获版本并复制草稿，后续目录刷新不覆盖草稿；409/428 保留草稿，重新读取必须明确确认。关闭或切页不取消已提交写操作。成功 HTTP 响应仍须带有效保存回执才能关闭表单。

分组只选择已定义模型及固定/全部来源授权，不编辑平台映射；原始白名单、可见/调用规则和搜索能力保持独立。原始策略及设置表单携带 U4 版本条件，设置草稿为脏时不更新捕获版本。迁移检查报告只在浏览器本地读取，拒绝状态/凭据文件；报告不代表当前实例健康。

验收入口：`CODEARTS_U5_BROWSER=1 GOMAXPROCS=2 GOFLAGS=-p=2 go test ./internal/app -run '^TestUnifiedModelsBrowser$' -v -count=1`（在 `backend` 执行，先构建前端）。该用例使用真实应用装配、认证、存储及模拟平台，浏览器不访问真实上游。既有浏览器回归由 `scripts/frontend_e2e.py` 执行，可用 `--cases` 仅重跑指定用例。

## 用户工作空间样式

普通用户页面参考 Sub2API 的布局比例与卡片层次，保留本项目浅黄色主色。`src/styles/user.css` 只作用于 `.user-workspace` 和用户专属 `portal-*` 类；管理员页面继续使用原样式和 DOM 控制器。

- `UserConsole` 管理导航、请求、资料与密钥状态；共用 API 客户端生命周期仍归 `ConsoleApplication`。
- `UserDashboard` 展示当前范围的完整聚合，SVG 趋势图使用既有用量响应，不增加图表依赖或网络请求。`UserBudgetPanel` 只在仪表盘显示累计限额。
- `UserAccountCard` 只在个人资料页显示身份概览；资料、登录方式、权益分别由独立组件维护。
- `UserKeysPanel`、`UserModelsPanel`、`UserUsagePanel` 各自维护页面展示；共享 `UserStatCard`、`UserIcon` 和 `user-format.ts`。模型搜索、地址复制和历史页签均在本地完成。

`user-style-smoke` 使用隔离的真实认证和只读展示样本，检查八页的桌面、手机和明暗主题，并输出 32 张截图。资料、密钥、兑换与权限的实际读写继续由原有浏览器用例验证。详见 [用户界面验收](../docs/acceptance/USER-STYLE-2026-11-02.md)。
