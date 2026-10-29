**CodeArts 非流式非法 JSON 修复 · 2026-10-29**

修改前已定版：提交 `ee4ddfc9d8c2b6b70b67cd1aeedbfa913aadf05a`，标签 `snapshot-20261029-232406-before-nonjson-fix`。本机私有备份位于 `data/backups/snapshot-20261029-232406-before-nonjson-fix/`，包含源码归档、已验证的 Git bundle、当时运行二进制、状态与访问配置、校验和及恢复说明。回滚程序时应保留当前凭据与用量，避免覆盖已轮换的 Token。未向远程推送。

参考 Sub2API 原始补丁 [ab9987b：fail over on non-JSON 2xx responses](https://github.com/Wei-Shaw/sub2api/commit/ab9987b2e22d532abdda7091c57afe403ec299f1)。本项目只在 `internal/codearts/client.go` 的 `Client.Chat` 成功读取 HTTP 200 响应后增加 JSON 语法校验，共 5 行业务代码（含注释）。

非法 JSON 转为现有的 `UpstreamError{Status: 502}`，在任何客户端输出之前返回；已有账号调度、退避、重试上限、模型冷却与错误映射继续处理它。合法 JSON 使用原始字节返回，不重新序列化；错误信息不包含上游原始正文。没有增加配置开关或修改流式、WorkBuddy、Trae 的实现。

**验证结果**

- 定向测试共 24 个案例：6 种非法正文 × Chat/Responses/Messages 三种入口；4 种合法正文；故障切换和重试上限各一例，全部通过。
- 非法正文包括文本、HTML、空响应、纯空白、截断 JSON、多个 JSON 拼接。最终失败返回 502、记失败、释放租约，不把账号标为凭据故障。
- 先坏后好：模型冷却后换另一个账号，客户端输出尚未提交，最终只收到成功响应；记录两个尝试，保留失败尝试用量未知的状态，不将其伪装为零消耗。
- 合法工具调用、refusal、纯推理、普通正文，以及未知结束原因、扩展字段、大整数和格式空白，均按原始字节保留，没有额外重试。
- 修改前执行相同新测试，Chat 和 Messages 的 12 个异常案例及两个重试案例失败，复现旧缺陷；Responses 的既有转换路径已能返回失败，但此前并未因此进入上游故障切换。
- `go test -race -count=1 ./...`、`go vet ./...`、格式/差异及前端静态检查通过。补强账号切换断言后，该用例再次通过竞态测试。
- 浏览器在无账号的独立实例验证设置隔离、保存/重载、明暗主题和移动布局，无真实上游调用，实例已停止。

**性能与重试影响**

使用同机、同一测试夹具对比定版前后 `Client.Chat` 路径；模拟 HTTP 响应消除网络波动，三次各 500ms 的基准取中位数。大小指正文内容长度，另有少量 JSON 包装。

| 响应大小 | 修改前 | 修改后 | 增加 |
|---|---:|---:|---:|
| 1 KiB | 0.0252 ms | 0.0300 ms | 0.0048 ms |
| 1 MiB | 0.8305 ms | 1.8064 ms | 0.9759 ms |
| 8 MiB | 3.8414 ms | 7.1424 ms | 3.3010 ms |

正常请求没有新增网络往返，校验开销随响应大小增加。异常请求现在可能触发现有 1 秒、2 秒等退避等待及后续上游尝试，因此其等待和消耗可能增加；本次没有提高尝试上限或新增重试机制。基准不代表真实网络延迟或并发容量。

**部署与真实请求**

确认在途请求为零后重启服务。运行二进制 SHA256 为 `025b3653de37e96428279be384054fc1bc17e62f81573747c0e9ea9437dd9ca0`，与构建文件一致；健康检查通过，账号数量和设置摘要保持不变。

一次真实 CodeArts `deepseek-v4.1-flash` 非流式请求原样返回 `11128; cc_version=42;`，HTTP 200、finish_reason=stop、1 次上游尝试、耗时 1.577 秒；回报输入 22 / 输出 9 Token。专用临时密钥已删除。这个样本仅验证部署后的正常路径，不作为长期成功率或前后性能证明。

**明确边界**

本轮只检查 JSON 语法，不引入完整响应结构校验。`{"choices":[]}`、JSON `null`、尚未被原有业务错误识别器覆盖的合法 JSON 错误对象，仍不由这个补丁识别。此范围与所参考的 Sub2API 小补丁一致；不宣称所有“HTTP 200 伪成功”都已解决。

证据：[定向验证](evidence/nonjson-fix-2026-10-29/targeted.log)、[旧版对照](evidence/nonjson-fix-2026-10-29/baseline-tests.log)、[竞态回归](evidence/nonjson-fix-2026-10-29/race.log)、[性能统计](evidence/nonjson-fix-2026-10-29/benchmark-summary.json)、[真实请求](evidence/nonjson-fix-2026-10-29/live.json)、[浏览器检查](evidence/nonjson-fix-2026-10-29/browser-smoke.log)。
