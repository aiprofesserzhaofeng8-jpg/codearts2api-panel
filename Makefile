# 常用开发命令。Windows 上可在 Git Bash 中运行，或直接复制命令执行。
#
#   make check     # 提交前必跑：vet（含跨平台）+ test + 控制台检查（gofmt 由 CI 强制）
#   make build     # 带版本号的本地构建
#   make race      # 并发竞态检查（需要 cgo）
#   make docker    # 构建镜像

GO      ?= go
PYTHON  ?= python3
BIN     ?= codearts2api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet vet-linux fmt lint race console architecture check clean docker

all: check build

frontend-install:
	./scripts/frontend.sh install --frozen-lockfile

frontend: frontend-install
	./scripts/frontend.sh typecheck
	./scripts/frontend.sh build

frontend-check: frontend
	./scripts/frontend.sh test
	$(PYTHON) tools/check_api_types.py

build: frontend
	cd backend && $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(abspath $(BIN))" ./cmd/panel

test: frontend
	cd backend && $(GO) test ./...

vet: frontend
	cd backend && $(GO) vet ./...

# 跨平台预检：在 Windows 上也能抓到「只在非 Windows 编译的文件」（如
# session_other.go）里的错误，以及「只在 Windows 定义、却被通用测试引用」的
# 符号 —— CI 的 linux job 就是挂在这类问题上。
vet-linux: frontend
	cd backend && GOOS=linux GOARCH=amd64 $(GO) vet ./...

fmt:
	cd backend && $(GO) fmt ./...

# 可选：需要先安装 golangci-lint（CI 不强制依赖它）。
lint:
	cd backend && golangci-lint run ./...

race: frontend
	cd backend && $(GO) test -race ./...

console: frontend
	$(PYTHON) tools/check_html_js.py

architecture: frontend
	$(PYTHON) tools/check_architecture.py --go "$(GO)"
	$(PYTHON) -m unittest discover -s tools -p test_check_architecture.py

check: frontend-check vet vet-linux test console architecture

clean:
	rm -f $(BIN) $(BIN).exe

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BIN):$(VERSION) .

.PHONY: frontend-install frontend frontend-check dev e2e bench
dev:
	$(PYTHON) scripts/dev.py

e2e: build
	$(PYTHON) scripts/frontend_e2e.py --binary "$(abspath $(BIN))"

bench: frontend
	cd backend && $(GO) test ./internal/catalog ./internal/gateway ./internal/scheduler ./internal/storage ./internal/usage -run "^$$" -bench . -benchmem
