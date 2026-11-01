# 常用开发命令。Windows 上可在 Git Bash 中运行，或直接复制命令执行。
#
#   make check     # 提交前必跑：vet（含跨平台）+ test + 控制台检查（gofmt 由 CI 强制）
#   make build     # 带版本号的本地构建
#   make race      # 并发竞态检查（需要 cgo）
#   make docker    # 构建镜像

GO      ?= go
PYTHON  ?= python
BIN     ?= codearts2api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet vet-linux fmt lint race console check clean docker

all: check build

build:
	cd backend && $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(abspath $(BIN))" ./cmd/panel

test:
	cd backend && $(GO) test ./...

vet:
	cd backend && $(GO) vet ./...

# 跨平台预检：在 Windows 上也能抓到「只在非 Windows 编译的文件」（如
# session_other.go）里的错误，以及「只在 Windows 定义、却被通用测试引用」的
# 符号 —— CI 的 linux job 就是挂在这类问题上。
vet-linux:
	cd backend && GOOS=linux GOARCH=amd64 $(GO) vet ./...

fmt:
	cd backend && $(GO) fmt ./...

# 可选：需要先安装 golangci-lint（CI 不强制依赖它）。
lint:
	cd backend && golangci-lint run ./...

race:
	cd backend && $(GO) test -race ./...

console:
	$(PYTHON) tools/check_html_js.py

check: vet vet-linux test console

clean:
	rm -f $(BIN) $(BIN).exe

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BIN):$(VERSION) .
