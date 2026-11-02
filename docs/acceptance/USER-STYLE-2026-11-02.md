# 用户工作空间样式验收

2026-11-02；本地版本 `0.3.0-user-style`，前端构建 `62296b7f87d2ebb9829d`。结构化结果见 [JSON](USER-STYLE-2026-11-02.json)，组件职责见 [前端说明](../../frontend/README.md)。

## 参考与范围

核对 Sub2API 官方仓库提交 `4a4a83ee5ec86b19d3a2dd9b6471de5128963bec` 中的 [公共布局](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/frontend/src/components/layout/AppLayout.vue)、[资料页](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/frontend/src/views/user/ProfileView.vue)、[仪表盘](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/frontend/src/views/user/DashboardView.vue)、[用量页](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/frontend/src/views/user/UsageView.vue) 和 [兑换页](https://github.com/Wei-Shaw/sub2api/blob/4a4a83ee5ec86b19d3a2dd9b6471de5128963bec/frontend/src/views/user/RedeemView.vue)。本次独立实现其布局比例、留白、卡片层次和表格密度，没有引入第三方项目源码或组件依赖。

保留此前选定的浅黄色主色，提供中性的浅色背景与深色面板。并非像素级复制：产品名称、菜单、能力范围、品牌与主色仍以本项目为准。

## 页面变化

| 页面 | 当前布局 |
|---|---|
| 公共用户骨架 | 256px 侧栏、紧凑顶栏、头像与实际余额、统一边距；普通页面最大 1440px，资料与权益等页面最大 950px |
| 仪表盘 | 四个统计卡、请求趋势 SVG、常用模型分布、当前记录页明细及独立累计额度卡 |
| 个人资料 | 渐变身份卡、余额/并发/注册时间概览、头像和昵称的双栏编辑卡 |
| API 密钥 | 独立接入地址区、创建工具条、密钥操作表格、无密钥时的引导 |
| 模型目录 | 搜索与模型卡片，复制精确模型 ID；不以展示权限冒充上游健康 |
| 用量 | 三个聚合统计卡、请求记录表、分页和计量说明 |
| 权益与兑换 | 余额/预留/套餐卡、兑换与订阅双栏、兑换记录和额度流水页签 |
| 登录方式与密码 | 邮箱/GitHub/LinuxDO 独立绑定行；居中密码表单，保留真实启用状态及原流程 |

移除每页重复的大块身份/预算面板。累计预算只在仪表盘显示；身份概览只在资料页显示。请求趋势与统计使用当前时间范围的完整后端聚合，不以当前分页代替总量；记录表明确标注为当前记录页。

## 验证

- `make check GO=/home/white/.cache/codearts2api-toolchain/go/bin/go` 通过：17 项前端单测、类型和生成契约、Go 全包测试与 vet/Linux vet、静态资源/CSP、架构和状态转换检查。
- 最终候选二进制执行 `scripts/frontend_e2e.py`，15/15 通过，包括原三平台/账号/成长任务界面、分组、配置、用户、权益和统一认证用例。实际资料保存、头像压缩、密钥显示与操作、兑换、密码修改和权限边界使用隔离实例的真实 HTTP/存储。
- 新增 `user-style-smoke`：八页 × 1440/390px × 明暗主题共 32 个填充数据状态通过；另外八页均检查 320px 和长昵称。检查模型搜索/复制、接入地址复制、流水切换、资料卡宽度、页面无横向溢出、无 Page/CSP 错误和用户页面不请求管理数据。普通用户请求管理接口返回 403。
- 手工目检资料、仪表盘、密钥、模型、用量、权益及登录绑定的桌面或手机截图。视觉填充数据使用只读样本，登录是真实隔离会话；样本中的余额、姓名和模型不是生产账号数据。
- 更新后的运行服务返回本次构建，JS/CSS 与本机构建文件逐字节一致。原管理员密码统一登录 200、管理读取 200、匿名/伪造角色/旧密码请求头 401、旧角色登录路径 404、管理员访问用户资料 403、退出后 401。

原始日志及截图位于忽略入库的 `artifacts/user-style/`。没有真实上游推理、领奖、SMTP 或第三方 OAuth 请求。

## 维护、性能与更新

只修改前端、测试和说明，没有后端、schema、网关、调度或平台协议改动。各页为独立 Vue 组件，公共图标、统计卡和格式化函数复用；用户样式隔离在 `.user-workspace` 与独有 `portal-*` 类中，管理员原生 DOM 所有权不变。API 生命周期仍归 `ConsoleApplication`。

仪表盘复用原有用量响应，搜索和复制为本地交互；未新增自动轮询、每请求网络调用、图表库、远程字体或运行依赖。页面资产有增加，本轮未进行完整模型时延或前端性能基准测试，不声称速度提升或零开销。

本地 `http://127.0.0.1:10088` 已更新，版本 `0.3.0-user-style`，验收时 PID 34367。停止服务后备份最新状态、账本、恢复文件和旧二进制到 `data/backups/user-style-final-20261102T175914/`，随后替换程序并启动。若需回退只换旧程序，不能覆盖最新凭据与账本。

逐项摘要比较确认完整 accounts/groups/keys/users/settings/consumer 及访问恢复文件保持一致；schema 10、4 个上游账号、3 个分组、5 把密钥、1 个普通用户及用量序号 32 均保留。仅更新本地，未推送 GitHub、未部署云端。服务重启后需按原账号密码重新登录。
