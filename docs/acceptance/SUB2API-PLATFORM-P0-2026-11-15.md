# Sub2API 平台扩展 P0 验收

日期：2026-11-15。结论：P0 复用准备完成；新增平台 P1 未开始。

本地源码基线：`d191c5deaeef8af189fffb5bfdfda242aea35db2`。
上游参考：`530c5e3a4f9c73ca2b0881191d0ef8e0d82c5484`。

交付：[复用与接入清单](../architecture/sub2api-platform-expansion/UPSTREAM.md)、[22 个来源文件清单](../architecture/sub2api-platform-expansion/upstream.json)、[主计划状态](../architecture/SUB2API-PLATFORM-EXPANSION-PLAN.md)。

## 实际执行

环境：Linux amd64，Go 1.27.3，GOMAXPROCS=2，GOFLAGS=-p=2。

```bash
GOMAXPROCS=2 GOFLAGS=-p=2 make check \
  GO=/home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go
```

退出码 0。前端 typecheck/build、12 文件 66 项前端测试、Go vet/test、控制台资源检查、架构边界、Node 登录辅助测试、模型审计脚本、104 项部署测试、状态转换测试通过。`make check` 的 Go 测试命中缓存，故另外对现有全部平台执行无缓存回归：

```bash
cd backend
GOMAXPROCS=2 GOFLAGS=-p=2 /home/white/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/go test -count=1 \
  ./internal/codearts ./internal/upstream/codearts ./internal/workbuddy \
  ./internal/trae ./internal/qoder ./internal/kiro ./internal/chatgpt \
  ./internal/claude ./internal/teleagent ./internal/mobileclaw
```

| 平台 | 本轮无缓存回归 |
|---|---|
| CodeArts（原生与 upstream 包） | 通过 |
| WorkBuddy | 通过 |
| Trae | 通过 |
| Qoder | 通过；仅模拟回归，不处理真实账号 403 |
| Kiro | 通过 |
| ChatGPT | 通过 |
| Claude | 通过 |
| TeleAgent | 通过 |
| MobileClaw | 通过 |

来源核对：GitHub 固定 SHA 下载 22 文件；SHA-256、字节长度、imports 与公开清单一致。根 COPYING 请求 404，已更正主计划。文档相对链接与 JSON 格式检查通过，`git diff --check` 通过。

## 证据和限制

本机日志（临时文件，可能被系统清理，不是验收复现的必要输入）：

- `/tmp/codearts-platform-p0-check.log`；SHA-256 `2943621d831800f70688c8219be3183c394e93b6a0050a4ceba4dd8af50044e2`。
- `/tmp/codearts-platform-p0-providers.log`；SHA-256 `e9e000acf5ba4534e438623a0f7b12b0c24b27411a09d8404fe2fce52a70f522`。

- 未新增适配器、未访问生产账号状态、未调用付费模型、未改运行服务。
- 未运行新的真实平台请求、浏览器端到端测试、race、Windows 运行测试或 Docker 构建；本轮只有文档/来源清单，已有本地测试通过不等于这些路径验证通过。
- 已完成来源和依赖审查；尚无实际复制的生产代码，最终片段许可、修改摘要与制品随附验证在 P1 执行。
- 不声明四个官方 API 平台已经可用，不把模型发现端点约定当成实测结果。
- 未提交/推送 GitHub、未发布 Release。P1 实施与发布分别验收。
