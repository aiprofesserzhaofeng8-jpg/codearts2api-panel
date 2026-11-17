# 添加账号改版验收

2026-11-18，本地源码与隔离实例验收完成。本文记录源码验收；Git 提交与推送以仓库历史为准，尚未发布或升级运行服务。

## 实现

- 桌面 11 个常用平台与展开入口按四列三行排列；展开后共 20 个顶层平台。CodeArts 仍为首次默认选择，重新打开保留现有平台选择规则。
- `frontend/src/features/accounts/brands.ts` 集中品牌名、顺序、本地图标与账号类型映射；AWS Bedrock 在 Claude 下，Vertex AI 在 Gemini 下，筛选器仍分别保留 22 个后端标识。
- OpenAI/Claude 的订阅与 API、Gemini API/CLI/Vertex、Coding 套餐、OpenCode Zen/Go、Command Code 套餐分别选择，复用现有导入和授权接口。
- 可选名称移至第二步。返回保留本次输入；真正切换带凭据的路径需确认，先释放旧会话再清空。取消失败保留原路径；保存失败保留输入，成功清空。异步二维码/本机登录结果保留已有会话身份检查。
- 弹窗宽 860px、高度不超过 85dvh，页头/操作固定、内容滚动；支持展开摘要、键盘单选、Escape 与关闭后焦点返回。
- 图标与许可见 `frontend/src/assets/providers/README.md`；生产静态资源包含 `provider-icon-notices.txt`。CodeArts、MobileClaw、TeleAgent、TypeSafe 使用自绘类别图标。

## 验证证据

- `./scripts/frontend.sh check`：TypeScript 检查及 67 个单元测试通过。
- `./scripts/frontend.sh build`、Go panel 构建、`python3 tools/check_html_js.py`、`python3 tools/check_api_types.py` 通过。Vite 保留现有主包超过 500kB 提示。
- 以下 22 项浏览器用例通过（真实页面、临时服务，平台 API 为模拟响应）：
  - account-create、accounts-table、vertex、bedrock、official-api、gemini-cli、subscriptions。
  - opencode、commandcode、cline、typesafe、antigravity、grok、mobileclaw-login。
  - qoder、kiro、teleagent、teleagent-login、teleagent-connector、providers、trae、trae-device。
- account-create 检查顺序/行数、20 平台展开、云渠道、键盘与焦点、同路径返回、跨类型确认、会话取消失败重试、保存失败重试、OAuth 配置缺失和无效链接隐藏；检查 localStorage 不包含表单测试密钥。
- MobileClaw 用例保留迟到轮询响应和取消检查；TeleAgent connector 用例验证本机任务取消，不保存未确认账号。
- 已查看 1280px 桌面与 360px 手机、中英文、明暗主题、展开及 Vertex 配置截图。产物在忽略的 `artifacts/onboarding-*.png`，测试日志位于 `artifacts/frontend-e2e/`、`artifacts/onboarding-e2e/`、`artifacts/onboarding-final/`、`artifacts/onboarding-visual/`。

未使用真实凭据登录、刷新或发起付费推理。本次没有后端业务改动，未执行全量 Go test/race。临时验收二进制为 `/tmp/codearts-onboarding`，不作为运行服务升级包。

## 图标与展开入口修正

用户反馈后，展开时隐藏“更多平台”，只保留“收起”；收起后恢复入口，隐藏按钮不进入焦点顺序。展开焦点转到展开区首个平台，收起焦点返回入口。

CodeArts、MobileClaw、TeleAgent、TypeSafe 已替换为各自官方产品图标（来源与权利归属见图标 README），保留原色，深色主题不再对这些图标反色。67 个单元测试、构建、页面静态检查及 account-create 浏览器回归通过；查看了桌面和 360px 手机的展开效果。

## 连续网格与品牌配色修正

常用和展开区共享同一 CSS grid，展开后 20 个平台连续四列五行，无中间空槽；收起按钮独占底部一行。图标优先使用 Lobe Icons 彩色版本，配 32px 浅色圆角底；黑白品牌保持原色。67 个单元测试、类型检查、构建和 account-create 浏览器回归通过，回归新增逐项网格坐标与图标加载检查。已查看桌面浅色/深色及 360px 手机展开截图。
