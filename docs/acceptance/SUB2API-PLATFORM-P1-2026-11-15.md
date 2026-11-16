# P1：国内官方 API 平台验收

日期：2026-11-15。状态：实现完成、隔离验收通过；真实 API Key 验收待完成，未部署/推送/发布。

基线：d191c5deaeef8af189fffb5bfdfda242aea35db2；本轮未提交工作区。
复用 Sub2API 固定版本：530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484。

## 交付

四个平台 `deepseek`、`zhipu`、`kimi`、`minimax` 已注册。每个平台有独立构造包，四者重复的无状态 HTTP/Chat SSE 逻辑共用 internal/cnapi；未新增 SDK 依赖、数据库、定时器、重试层或文件存储。

- 国内官方 payg API Key，稳定平台隔离身份摘要、NonExpiring；拒绝 OAuth、自定义端点和错误套餐/地区；接口和页面均不提供虚假浏览器登录。
- 动态目录；仅智谱/MiniMax 的目录 404/405 使用官方有限列表，导入时额外短生成校验；鉴权/限流/服务错误不降级成假成功。
- 现有静态密钥导入协调器、严格保存、凭据使用、目录归并和权限继续复用。补齐平台白名单、别名持久化、共享来源、默认策略、账号标签及筛选。
- DeepSeek 多币种余额、Kimi CNY 余额；未知/非法数值报错，真实 0 保留；另两平台明确暂不支持查询。
- 上游 Chat Completions，客户端既有三协议转换。reasoning_content、工具 ID/参数/历史、图片内容块保留；无静默换型号或丢弃输入。
- 非终止 EOF、空流、业务错误帧报错；尊重取消与 emit 错误；不把临时余额不足永久标为认证故障。
- 原始和共享名称沿用现有精确归并，停用选择不覆盖，分组 ACL 不扩大。
- 来源记录及 LGPL/GPL 文件加入源码、Linux/Windows 打包清单和 Docker COPY。

用法见 [官方 API 接入](../OFFICIAL-APIS.md)，准确来源及本地变更见 [cnapi/UPSTREAM.md](../../backend/internal/cnapi/UPSTREAM.md) 和 [机器清单](../../backend/internal/cnapi/upstream.json)。

## 验证证据

| 检查 | 结果和范围 |
|---|---|
| make check | 通过：前端类型/构建/66 项测试、Go vet/test、架构检查、Node/Python/104 项部署回归；第一次捕获旧九平台数量断言，更新到十三平台后完整通过 |
| 四平台原始/共享名称 × 三 API × 流/非流 | 48 组合通过；真实 HTTP 转换+真实适配器，仅上游为 httptest；每次调用一次、计量 12/4/3、来源准确、在途归零 |
| 相同 48 组合分组撤权 | 空分组返回 403，未产生第二次上游请求；含共享名称不能绕过 ACL |
| 四平台共享目录自动同步 | 首次发现、追加、保留模型与来源停用、ACL 不变、幂等与重开持久化通过 |
| 新平台 Adapter -race | 通过；四平台认证/静态身份/HTTP、工具及 reasoning 流逐字节保留、大整数、图片传递、空流/断流、401/403/429/500、404 备用目录校验、重定向拦截、取消、超时、emit 失败 |
| 保存失败 | 使用真实 ProviderLogins + 实际 Adapter，模拟磁盘失败；失败不保存，重试成功；未另建保存服务 |
| 余额回归 | 缺失/空/非法/NaN 不变零，真实零保留、双币种分离、Kimi 非零业务码失败、未知余额不发额外网络请求 |
| 浏览器 | official-api-smoke、account-create-smoke、subscriptions-smoke、quota-smoke 全通过；新流程中文/英文，密码框、无 OAuth、失败保持表单、成功清理密钥 |
| 构建 | Linux amd64 测试二进制构建通过；Windows amd64 交叉编译通过（未在 Windows 运行） |
| 制品许可 | Linux/Windows 打包测试 4 项通过，归档包含来源及许可文件并校验摘要；Docker COPY 已补齐，未构建/运行镜像 |

执行时使用 GOMAXPROCS=2、GOFLAGS=-p=2，工具链 Go 1.27.3。完整命令：

```bash
GOMAXPROCS=2 GOFLAGS=-p=2 make check GO=/home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go
# 测试新适配器及共享来源链路；从 backend 执行
GOMAXPROCS=2 GOFLAGS=-p=2 go test -race ./internal/cnapi -count=1
GOMAXPROCS=2 GOFLAGS=-p=2 go test -race ./internal/server -run TestOfficialAPI -count=1
# 使用本轮构建的临时程序，不启动生产数据
python3 scripts/frontend_e2e.py --binary /tmp/codearts-p1-panel --cases official-api-smoke account-create-smoke subscriptions-smoke quota-smoke --output /tmp/codearts-p1-browser
python3 -m unittest discover -s deploy/tests -p 'test*package*.py'
```

本机临时证据：`/tmp/codearts-p1-check.log`、`/tmp/codearts-p1-adapter-final.log`、`/tmp/codearts-p1-acl-race.log`、`/tmp/codearts-p1-browser/browser-results.json`、`/tmp/codearts-p1-packaging.log`。日志可能被系统清理，回归可用上述命令重做。

## 真实验收矩阵

| 平台 | 认证/范围 | 来源 | 隔离结果 | 真实文本/工具/图片/用量/共享客户端 |
|---|---|---|---|---|
| DeepSeek | API Key / cn / payg | deepseek/cn/{原型号} | 通过 | 未执行：未提供官方密钥 |
| 智谱 | API Key / cn / payg | zhipu/cn/{原型号} | 通过 | 未执行：未提供官方密钥 |
| Kimi | API Key / cn / payg | kimi/cn/{原型号} | 通过 | 未执行：未提供官方密钥 |
| MiniMax | API Key / cn / payg | minimax/cn/{原型号} | 通过 | 未执行：未提供官方密钥 |

未读取生产 state.json 或寻找其他应用密钥。现有 WorkBuddy/Qoder 等订阅凭据不能替代官方 API Key。本轮未尝试收费请求，页面提示仅说明未来管理员导入时的行为。

下一步真实验收：用户在新版本面板导入自己的国内 payg API Key，以临时限定分组逐平台测试共享名称、随机标记文本、工具多轮及明确支持图片的型号；日志核对实际来源、用量、租约释放后删除临时资源。目录和静态能力资料不代替此步骤。Coding Plan、国际站、最大上下文、所有模型可用性、厂商原生 Responses/Messages 不在本批通过声明中。

P1 工程工作已交付本地；不能据此将全部平台扩展 P0～P6 或真实验收标为完成。
