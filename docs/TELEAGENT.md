# TeleAgent 接入

`teleagent` 对应中国电信 TeleAI 的 TeleAgent，地区为 `cn`。账号池提供「TeleAgent → 浏览器登录 / 凭据导入」，检查时只读查询官方身份，浏览器登录需预览后显式确认保存。模型请求走 TeleAI 网关，复用现有账号调度、权限、预算、用量账本和协议转换。

## 浏览器登录

日常流程：**点击浏览器登录 → 在官方页面认证 → 自动显示账号预览 → 确认保存**。正常流程不下载任务、不上传结果 JSON。账号池仍只在最后确认时写入。

### 首次连接本机组件

这一步在你自己的电脑执行一次，面板可以部署在本地或远程服务器。需要 Node.js 22.15+、Chrome / Chromium / Edge，并先安装、启动一次 TeleAgent 官方客户端。

1. 在「添加账号 → TeleAgent → 浏览器登录 → 下一步」展开「首次连接 / 组件设置」，下载登录组件。
2. 在下载目录执行页面提供的安装命令。下面以本机面板为例；远程面板须使用自己的 HTTPS origin：

   ```sh
   node teleagent-login.mjs --install --origin "http://127.0.0.1:10087"
   ```

3. 将终端显示的**连接码**填入面板的组件设置，点击「连接本机组件」。仅在此面板填写，不发送到聊天。浏览器询问本地网络访问时需允许。
4. 安装器将脚本复制到用户目录 `~/.codearts2api-login`，保存该面板 origin 与连接密钥，启动后台组件。Linux 使用桌面自启动文件，Windows 使用当前用户 Startup VBS；以后登录桌面自动启动。无需 sudo，不安装系统级服务。

组件没有管理员 Cookie 或管理 API Key。连接码仅用于浏览器访问本机组件，保存在该面板 origin 的浏览器 localStorage；清理浏览器存储、更换浏览器或解除配对后需要再次连接。`localhost` 与 `127.0.0.1` 是不同 origin，应使用首次连接时的地址。

### 日常添加账号

1. 在面板选择 TeleAgent，点击「浏览器登录」。
2. 组件自动读取本机 TeleAgent 的网关参数、真实版本和原设备信息，打开独立官方登录窗口。
3. 在官方页面完成认证，面板自动收到结果并向官方核对身份，显示账号预览。
4. 核对后点击「确认保存账号」。已有账号仍需明确勾选更新凭据。

授权结果仅在内存交接，不落地结果文件。浏览器先向已配对的本机组件读取结果，再通过已有管理员认证的面板接口核验；没有新增公网回调或免认证保存接口。取消、关闭或退出面板使本次等待停止，组件丢失页面心跳后最多约 20 秒取消窗口并清理结果。账号自然到期仍需重新登录，没有已确认的自动续期协议。

### 连接与维护

组件固定监听 `127.0.0.1:18765`。请求同时校验准确 Host、配对面板 Origin、连接密钥和本次任务；其他网站、错误任务和未配对请求会被拒绝。面板 CSP 仅增加这个明确的 loopback 地址，未放开任意本地或外部连接。远程面板要求 HTTPS，浏览器的本地网络访问限制可能需要用户确认。

重新运行安装命令可更新组件，并保留同一面板配对。改连其他面板前先卸载。可用以下命令检查客户端元数据或卸载组件：

```sh
node teleagent-login.mjs --check
node teleagent-login.mjs --uninstall
```

卸载只删除本机组件和自启动文件，不删除 TeleAgent 官方客户端或面板账号。macOS 暂未提供自动安装；可前台运行 `node teleagent-login.mjs --serve --origin https://你的面板域名`，保持进程运行并按显示的连接码配对。

### 自动读取范围

只检查固定的 TeleAgent 安装位置、应用 ASAR 索引和选定的公开 JavaScript 资源，以及设备级 `app-auth/device-meta.json`。网关参数只接受签名 NewApi 模型提供者的配置，排除图片/搜索等其他通道；版本来自实际 `package.json`。不执行应用代码，不读取旧账号 Token、浏览器 Cookie、系统钥匙串或进程内存，不生成替代设备身份。

本机已只读验证 TeleAgent 3.0.0-rc.4 的四项元数据。跨平台默认路径包含 Windows 的 Program Files / LocalAppData 和 macOS 的 Applications。非默认安装可用前台模式的 `--app /客户端目录`、`--data-dir /客户端数据目录` 指定；缺客户端、设备记录、多组不同配置或无法识别版本时明确停止，不随机猜测。

### 高级：手动文件交接

旧文件方式保留在折叠的高级选项里，仅供组件通信不可用时使用。先生成手动任务并下载，在自己电脑运行页面给出的命令，再选择结果文件并检查、确认保存。默认自动读取客户端元数据；也兼容 `--config CLIENT.json` 手工配置。

手动结果包含敏感凭据，POSIX 下以 0600 独占创建，拒绝覆盖已有文件；Windows 沿用用户目录权限。保存账号后删除结果文件。正常自动登录不需要这些文件。

## 凭据导入

先在官方客户端登录，通过自己获准使用的导出或配置取得完整凭据。在面板中选择「添加账号 → TeleAgent → 凭据导入 → 下一步」，将凭据 JSON 填入导入框，校验通过后保存。

```json
{
  "token": "<本人账号令牌，可带 tenant: 前缀>",
  "api_key": "sk-<本人获准使用的网关密钥>",
  "device_id": "<原客户端 deviceId>",
  "install_id": "<原客户端 installId>",
  "app_version": "<原客户端版本，例如 2.7.1>"
}
```

`token` 是账号令牌，发到 `X-Token`；`api_key` 是模型网关认证，发到 `Authorization`。二者用途不同。程序不内置第三方共享网关密钥，不会生成或轮换设备信息，不读取桌面进程、浏览器 Cookie 或系统钥匙串。

可使用 `access_token` / `accessToken`、`apiKey`、`deviceId`、`installId`、`appVersion` 字段别名，也支持包在 `auth` 对象中。`account_id` 和 `expires_at` 优先从 JWT 声明补齐；显式过期时间不能延长 JWT 的 `exp`。身份最终通过官方 `GET /superCowork/sapi/cowork/user_info` 复核，JWT 解析本身不等于签名校验。

浏览器登录需要上述本机助手；没有已确认的自动续期协议。账号不标成长期有效或可续期；到期需重新登录并显式覆盖原账号凭据。现有凭据协调器保留到期前 5 分钟的安全余量，进入该余量后也会要求更新凭据。

## 调用与模型目录

对外仍使用已有三个入口：

- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/messages`

原始型号为 `teleagent/cn/chat-pro`、`teleagent/cn/chat-flagship`、`teleagent/cn/chat-flash`、`teleagent/cn/chat-lite`、`teleagent/cn/chat-nano`。它们来自固定参考版本的客户端预置目录，不代表已验证账号对每个型号的调用权限；没有可确认的动态目录端点，未宣称最大上下文、图片、底层厂商或实际模型版本。

例：调用 Chat Completions 时指定 `"model":"teleagent/cn/chat-pro"`。也可以在账号分组中将它映射到带版本的公开名字，映射与原始型号仍受同一个分组权限入口约束。不会自动把 TeleAgent 的产品档位映射成某个厂商型号。

TeleAI 模型网关使用两阶段 HMAC-SHA256，签名绑定原账号令牌、安装标识、实际发送的请求路径/字节、秒级时间、nonce 和原客户端版本。用户消息、system/developer、工具参数和工具结果经现有策略后转发；服务不执行模型返回的工具。非流式客户端复用现有 SSE 聚合；缺失结束标记、错误帧或断流不伪装成成功。

## 积分与限制

默认查询 `GET /superCowork/sapi/user/portal/companyStore/userCenter` 的 `current_points_balance`。如果用户另行提供同一账号的 `operation_token`（可选 `oper_token`），改用独立积分门户 `GET /meter/super-agent/user/portal/points/detail` 的 `pointsBalance`；门户 Cookie 不发送到推理接口。

返回余额可为数值或十进制字符串，明确的 0 有效。只有余额时只展示余额，不编造已用量、总额或周期。未返回余额显示未知，业务失败显示错误；查询沿用原有缓存/期限/并发控制，不更新推理健康或账本。积分与本地 USD 参考费用互相独立。

每日积分已接入账号行「每日签到」、顶部批量领取以及「配置 → 活动福利自动领取」。只有上游明确返回 `claimed_today=false`、任务可见且可领积分大于零时才提交 `type=daily_login`；已经领取则直接返回。领取后复查 `claimed_today=true` 才记录成功并失效余额缓存；提交成功但复查失败或仍未领取，不标记已领，沿用现有 30 分钟等待期。自动领取开关不被本次升级强制开启。

默认使用账号业务 API 的 `/user/portal/companyStore/v2/checkDayTask` 和 `/user/portal/companyStore/checkTask?type=daily_login`；只有显式提供 `operation_token` 时才使用独立门户对应路径及其凭据。两套认证不混用，不自动回退到另一套身份。领取在上游是有副作用的 GET，因此只在管理员领取动作/已开启的后台任务中调用；列表刷新不会触发。领取专用连接禁用复用与 HTTP/2，避免传输层将该 GET 自动重放。

奖励值来自状态或领取前后累计积分差，未硬编码每天固定数量。邀请等活动若出现在状态响应中，仅展示为需满足条件的活动提示，不会当成可领取任务。当前 3.0.0-rc.4 客户端还包含 v3 活动展示与首登/月度赠送逻辑；这些要求核对相应条件、协议与结果，未接入无条件批量领取。详见 [积分领取范围](DAILY-REWARDS.md)。

成长任务、独立搜索和生图 API 尚未接入。导入身份成功不能证明网关密钥、全部模型权限或每种扩展参数已经通过真实调用。未使用真实账号验证登录完成、模型生成、积分或自然到期；自动测试使用模拟上游及隔离状态。官方 UPMS 入口仅匿名检查了可跳转到登录页面。

## 维护与来源

平台 HTTP/签名/凭据在 `backend/internal/teleagent/`；装配在 app，中立业务不依赖平台包。新设置为兼容字段，状态 schema 仍为 10；旧程序无法使用新平台，回退前需检查账号/分组配置的兼容性，不能盲目恢复旧凭据。

协议依据：[teleagent-proxy](https://github.com/liao96312/teleagent-proxy/tree/875a2b4080ded5f9eebada6d43c94c77ebddc5a5)、[dsh-connect-teleagent](https://github.com/anbulang/dsh-connect-teleagent/tree/14bb4bb16d6961addff63ced79866f9a74391108)、[dsh-codearts-auth](https://github.com/anye-99/dsh-codearts-auth/tree/686658793c982cd0e1baedbcfa10a396ce5d1a09)。三者协议记录存在版本差异，采用可交叉核对的网关签名事实，不采用伪造本地签名密钥的未验证假设。MIT 许可见根目录 `LICENSE.teleagent`，随 Linux、Windows 与镜像分发。

每日积分状态兼容 JSON 布尔值、0/1 及对应字符串；缺失或无法识别的状态不按未领取处理。积分门户 operation_token 与聊天凭据继续分开使用。新增字段兼容已通过隔离测试；2026-11-09 单账号真实领取与复查通过（100 积分），生产面板端到端流程尚待更新后验收。
