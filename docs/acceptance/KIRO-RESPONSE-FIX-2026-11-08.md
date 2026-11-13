# Kiro 响应识别修复验收

日期：2026-11-08。范围：Kiro 原生适配器响应处理。源代码与隔离测试完成；生产未部署，真实账号生成尚待验证。

## 故障证据

用户执行的原生诊断返回 HTTP 200、Content-Type application/json；首帧长度 141，prelude CRC 与 message CRC 均有效。原适配器在读取帧之前仅根据响应头拒绝，因此产生 502。首帧有效只能证明响应头判断有缺陷，不能证明后续事件成功，也不能据此判定订阅或额度正常。

## 本次实现

- `backend/internal/kiro/response.go`：按实际响应体区分 EventStream 与 JSON 错误。正常生成只增加一个小型缓冲读取器，仍逐帧解析、逐片输出。
- `chat.go`：去掉 Content-Type 硬性拒绝，调用上述识别入口。沿用原 HTTP 非 200 错误处理。
- `eventstream.go`：异常帧与 JSON 错误共用已知机器码映射。认证 401、权限 403、配额/支付 402、限流 429、参数 400；未知或上游内部异常 502。未知错误码及原始错误消息不对外透传。配额错误本身不能证明必须购买订阅。
- JSON 错误读取上限 64 KiB，额外一个字节只检测超限。损坏 CRC、半帧、空响应、未结束工具沿用失败语义，不重同步或伪造完成。
- 未新增状态字段、数据库、任务、响应正文落盘、模型替换、账号池或跨端点重试。Trae 不在本次范围。

行为参考的固定提交与许可边界见 [Kiro UPSTREAM](../../backend/internal/kiro/UPSTREAM.md)。实现前源码备份：`/home/white/backups/codearts2api/before-kiro-response-fix-20261108-173558/source.tar.gz`，保留此前未提交变更。

## 验证结果

以下均通过，Go 使用 GOMAXPROCS=2、GOFLAGS=-p=2：

1. Kiro 和 server 定向测试：标准/JSON/缺失/其他响应头的合法帧、机器码分类与脱敏、空或无效 JSON、CRC 损坏与半帧、不等 EOF 即交付、消费者取消。
2. 隔离假上游经网关 Chat Completions：JSON 响应头下文本与工具调用，分别测试流式/非流式；保留工具大整数，每次请求仅一次上游调用并释放租约。
3. `make check` 完整项目检查通过，日志 `/tmp/kiro-fix-check.log`。
4. `go test -race ./internal/kiro ./internal/server -run 'Test(Response|JSONErrorRead|Kiro)' -count=1` 通过，日志 `/tmp/kiro-fix-race.log`，包含有界错误体读取测试。
5. `git diff --check` 通过。

以上均为本地源码与假上游结果，不能作为真实 Kiro 生成通过的证据。

## 真实账号验证

已编译修复版验证工具 `/tmp/codearts-kiro-fixed-verify-20261108`。只读取现有状态，使用修复后的适配器直接请求官方端点；不请求本地网关、不刷新凭据、不保存账号或响应正文。查询额度和模型目录，随后测试 auto 与目录中的一个具体型号，使用随机标记判断是否实际生成。诊断输出仅包含状态、错误码、响应类型、分片数及标记匹配结果。

当前进程无法读取 `/opt/codearts2api/data/state.json`，且非交互 sudo 需要密码；已验证在此条件下不会发送请求。由本机用户执行：

```bash
sudo -u codearts2api /tmp/codearts-kiro-fixed-verify-20261108 /opt/codearts2api/data/state.json
```

源码与构建 overlay 保存在忽略目录 `artifacts/kiro-response-fix-2026-11-08/`。真实通过要求 generation 的 ok 与 expected_marker_received 同时为 true；仅 HTTP 200 或目录成功不足以通过。401 需先通过正常账号流程续期，再重新验证；403/402 需结合权限或额度结果判断。

本次没有替换生产二进制或重启系统服务，也没有提交、推送或发布 Release。
