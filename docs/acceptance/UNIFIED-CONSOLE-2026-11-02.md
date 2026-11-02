# 管理员与用户共用控制台界面

2026-11-02；版本 `0.3.0-unified-console`，前端构建 `5de9731627b0616ee05f`。结构化结果见 [JSON](UNIFIED-CONSOLE-2026-11-02.json)。

## 行为与边界

参考 Sub2API 的 [AppLayout](https://github.com/Wei-Shaw/sub2api/blob/main/frontend/src/components/layout/AppLayout.vue) 和 [AppSidebar](https://github.com/Wei-Shaw/sub2api/blob/main/frontend/src/components/layout/AppSidebar.vue)：共用布局骨架，按角色提供菜单。本项目保留浅黄色明暗主题，以自身组件实现，不引入 Sub2API 的依赖或后端结构。

- 管理员 `/` 与用户 `/user` 共用侧栏、顶栏、品牌、主题开关和登录卡片样式。侧栏底部提供管理空间与个人空间入口；桌面支持折叠，手机使用抽屉导航。
- 用户功能从正文横排菜单移入侧栏，支持 `/user#keys` 等直接链接、刷新保留页签、浏览器前进/后退。切页清除显示的密钥及密码草稿。
- ConsoleLayout 使用导航、标题、操作和状态插槽。管理员 Sidebar、AdminHeading、AdminSessionActions 保留独立 `v-once` 子树；用户分支保持响应式。主题和折叠操作不会覆盖原生控制器更新的标题、导航和表单草稿。
- 管理员路由事件只匹配主导航的 `button[data-tab]`，不再绑定用户权益页面内的导航按钮。
- 入口切换不合并身份或会话，不授予额外权限；管理员密码仍不能作为用户密码。用户页面不读取管理员快照，账号池和消费者身份继续分开。

## 验证

`make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` 通过，包括前端类型检查、16 项单测、Go 测试与 vet、跨平台 vet、资源/API 类型及架构边界检查。

`python3 scripts/frontend_e2e.py --binary bin/codearts2api-panel-unified --output artifacts/unified-console/final` 的 14 组浏览器验收全部通过。覆盖原账号添加、三平台授权界面、成长任务、分组映射、用量、用户资料、密钥、额度、订阅、兑换与登录配置，并新增共用布局专项。平台接口使用模拟数据；用户、权限、额度和会话流程使用隔离实例的真实 HTTP/存储，不使用真实账号凭据。

专项确认管理员/用户 Cookie 互不替代，用户请求管理员状态返回 401，管理员会话请求用户资料返回 401；用户功能页不发出管理员请求。还验证主题/折叠持久化、深链接/刷新/后退、手机各菜单、Escape 关闭、焦点返回、320px 无横向溢出和严格 CSP。桌面、手机及明暗主题截图在忽略入库的 `artifacts/unified-console/browser/`。

运行中的 10088 服务另做了真实浏览器检查：用户登录页、管理员登录页、入口切换、手机尺寸均正常，无页面或 CSP 错误。未登录生产账号、未执行真实上游请求或领取任务。

## 性能与部署

没有新增依赖，后端文件与推理路径无修改，没有新增推理网络请求、重试或逐帧处理。相对更新前正在运行的版本，JS/CSS 总原始大小从 367760 增至 380406 字节，增加 12646 字节；以相同 gzip 参数离线压缩增加 3790 字节。这是静态资源体积比较，不是实际网络时延或模型首字速度测量。

本地 `codearts2api-panel.service` 已更新，地址 `http://127.0.0.1:10088`，运行 PID `4070269`；嵌入前端构建标识与候选版本一致。停止旧进程后备份最新状态、结算账本、访问文件、成长状态及旧二进制，再替换程序。未恢复旧状态或凭据。

schema 保持 10，4 个账号、3 个分组、5 把密钥和 1 个用户均保留；账号凭据摘要一致，部署前后结算序号均为 32。只更新本地服务，未推送 GitHub 或更新云端。两类登录继续独立，尚未实现管理员与普通用户共享一个身份或模拟登录。
