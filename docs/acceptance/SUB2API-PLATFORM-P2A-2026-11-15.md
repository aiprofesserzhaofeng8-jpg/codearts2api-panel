# P2a：Gemini 官方 API Key 基础接入

后续更新：CLI OAuth/原生传输已实现并通过隔离验收，见 [P2b记录](SUB2API-PLATFORM-P2B-2026-11-15.md)。下文保留此前验收时点，Pi签名和真实Google验证仍未完成。

后续更新：网关跨协议签名已修复，本机 Pi 仍丢弃扩展字段，见 [补充验收](GEMINI-SIGNED-TOOLS-2026-11-15.md)。下文保留首次 P2a 验收范围。

日期：2026-11-15。状态：API Key 基础接入及下列隔离检查通过；**P2 未全部完成**，CLI OAuth、原生协议和签名工具跨协议、真实账户验收仍待完成。未替换运行服务、未推送或发布。

## 实现与复用选择

审阅 Sub2API 固定 `530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484` 的 Gemini 模型/工具辅助函数、Chat/Messages 兼容服务、thinking 和 URL 逻辑。逐文件摘要见 [清单](../architecture/sub2api-platform-expansion/gemini-upstream.json)。这批只审阅，没有将这些文件整段复制；已复用的 cnapi 错误分类代码仍带上一批 LGPL 来源和许可文件。

Sub2API 的服务绑定 Account/Gin/配置/调度，并通过 Chat→Responses→Anthropic→Gemini 转换。P2a 选择 Google 官方 OpenAI 兼容端点，复用本项目现有 Chat 转发，不引入第二套调度或转换链。CLI/Code Assist 无法用这个 API Key 端点代替，仍须 P2b 单独实现。

- 新增 `internal/gemini`，scope 固定 api；cnapi 只增加 scope 可由构造器指定，国内四平台默认 cn 不变。
- 目录使用原生分页 `/v1beta/models`，请求头传 key；读取精确型号及 input/output token 限额；鉴权失败、重复分页、无对话型号都不会保存为可用账号。
- 上游推理固定官方 `/v1beta/openai/chat/completions`，原有流边界、超时/取消和错误分类保留。
- 沿用 ProviderLogins 严格保存、现有共享目录自动同步、分组权限和租约释放。
- 页面新增 Gemini API 选项、api scope、隐藏密钥输入、明确“CLI 尚未接入”；无假 OAuth 按钮或余额。
- Chat 非流式聚合补保留工具 `extra_content`，避免丢失 Google thought_signature；流式本来就传递原始 chunk。无签名存储、文件服务、额外后台请求。

使用说明见 [Gemini API](../GEMINI-API.md)。

## 验证结果

| 检查 | 证据与限制 |
|---|---|
| 全量 make check | 通过：前端构建/66 测试、Go vet/test、架构、API 类型、部署和状态迁移回归 |
| 新平台三协议文本 | 原始/共享名 × Chat/Responses/Messages × 流/非流：12 组合通过；加上 P1 四平台共60组合；对应撤权403，无额外上游调用，缓存计量与租约正确 |
| Gemini 目录与身份 | 固定端点、静态身份、scope隔离、分页/去重/精确限额、跳过专用媒体/embedding、未知型号不猜能力、401/403/429/500/302及畸形/重复分页均有隔离回归 |
| 账号保存 | 真实 Gemini Adapter + ProviderLogins，原生目录验证后模拟磁盘失败；失败不保存，重试成功；验证过程不生成收费请求 |
| Chat 图片/签名工具 | 真实网关+适配器、模拟上游：共享名称两轮工具、extra_content签名、9007199254740993、图片块传递及工具结果回传通过；缓存5、推理3 token分别保留；在途归零 |
| 断流与取消 | 空流、业务错误帧、缺少结束帧不能成功；context取消传播；原有四平台传输回归通过 |
| 目录同步 | 新平台追加来源、保留停用状态、ACL不扩权、幂等和重开持久化通过；这是调度数据测试，不证明真实Gemini支持其他厂商型号 |
| 浏览器 | official-api-smoke及account-create-smoke通过，含中英文、五个API平台、Gemini api scope、无OAuth、保存失败重试及成功清理密钥 |
| 构建 | Linux构建通过；Windows amd64交叉编译通过，未在Windows实机运行 |

本机证据（临时日志可能被清理）：
- `/tmp/codearts-p2a-check.log`
- `/tmp/codearts-p2a-race.log`
- `/tmp/codearts-p2a-adapters.log`
- `/tmp/codearts-p2a-signed-tool.log`
- `/tmp/codearts-p2a-browser/browser-results.json`

关键命令（Go1.27.3；均使用GOMAXPROCS=2、GOFLAGS=-p=2）：

```bash
make check GO=/home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go
# backend目录
go test -race ./internal/gemini ./internal/cnapi ./internal/protocol -count=1
go test -race ./internal/server -run 'TestOfficialAPI|TestGeminiSharedChat' -count=1
# 项目根目录，使用临时构建及临时数据
python3 scripts/frontend_e2e.py --binary /tmp/codearts-p2a-panel --cases official-api-smoke account-create-smoke --output /tmp/codearts-p2a-browser
```

## 必须继续完成的边界

1. **Gemini 3 签名工具的 Responses/Messages 往返尚未完成**：现有类型化转换不能保证保留所有 Google extra_content。Chat通过不能代表这两种协议通过。当前 Gemini 3 工具能力暂不标已确认；不加入伪签名或自动换模型掩盖差异。
2. **P2b未实现**：CLI/Code Assist OAuth、项目初始化、刷新、原生内容/工具/用量转换，需要复用后续固定源码并单独验收；api与cli账号不能混用。
3. **无真实Gemini账户验证**：未读取生产state、未搜其他应用密钥、未对Google发起收费调用。隔离图片只证明传递，不能称已真实识图；未测Pi和极限窗口。
4. **交付状态**：本地源码与测试二进制，未替换v0.4.14服务，未GitHub提交/推送/Release；不把本轮结果当作整个P0～P6完成。
