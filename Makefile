# Open Anywhere 开发任务入口。运行 `make` 或 `make help` 查看全部目标。

SHELL := /bin/bash

GO          ?= go
PNPM        ?= pnpm
PORTAL_DIR  ?= portal
GATEWAY_BIN ?= bin/gateway
ARGS        ?=

.DEFAULT_GOAL := help

.PHONY: help deps tidy portal-install fmt fmt-check vet test cover e2e \
        portal-typecheck portal-build portal-dev check check-all \
        build build-gateway build-gateway-embed run run-casting run-render run-voiceentry clean

##@ 通用

help: ## 列出所有目标
	@awk 'BEGIN {FS = ":.*## "}; \
		/^##@ / {printf "\n%s\n", substr($$0, 5); next}; \
		/^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

##@ 依赖

deps: ## 安装 Go 与前端依赖
	$(GO) mod download
	$(MAKE) portal-install

tidy: ## 整理 go.mod / go.sum
	$(GO) mod tidy

portal-install: ## 安装前端依赖（严格按 lockfile）
	cd $(PORTAL_DIR) && $(PNPM) install --frozen-lockfile

##@ 检查与测试

fmt: ## 格式化 Go 代码
	$(GO) fmt ./...

fmt-check: ## 检查 Go 代码格式（只报告，不修改）
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "以下文件未通过 gofmt:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet: ## 运行 go vet
	$(GO) vet ./...

test: ## 运行全部 Go 测试（-race）
	$(GO) test ./... -race -count=1

cover: ## 运行测试并生成覆盖率报告 coverage.html
	$(GO) test ./... -race -covermode=atomic -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html

e2e: ## 运行 Render 视频 E2E（需要 Chrome、FFmpeg、lsof）
	$(GO) test -race -tags=e2e ./e2e -count=1 -timeout=180s

portal-typecheck: ## 前端类型检查
	cd $(PORTAL_DIR) && $(PNPM) typecheck

portal-build: ## 构建前端产物到 portal/dist，并同步到 Go 嵌入目录
	cd $(PORTAL_DIR) && $(PNPM) build

check: fmt-check vet test ## Go 格式检查 + go vet + 测试（等价 CI）

check-all: check portal-typecheck portal-build ## Go 与前端全量检查

##@ 构建

build: ## 编译全部 Go 包
	$(GO) build ./...

build-gateway: ## 编译 Gateway 二进制到 bin/gateway
	mkdir -p $(dir $(GATEWAY_BIN))
	$(GO) build -o $(GATEWAY_BIN) ./cmd/gateway

build-gateway-embed: portal-build ## 编译内嵌前端产物的 Gateway（单文件部署）
	mkdir -p $(dir $(GATEWAY_BIN))
	$(GO) build -tags portalembed -o $(GATEWAY_BIN) ./cmd/gateway

##@ 运行

run: ## 启动 Gateway（默认读取 gateway.yaml）
	$(GO) run ./cmd/gateway $(ARGS)

run-casting: ## 启动 Casting，例：make run-casting ARGS="-addr :8080 -interface en0"
	$(GO) run ./cmd/casting $(ARGS)

run-render: ## 启动 Render，例：make run-render ARGS="-addr :8090 -public-url http://192.168.1.20:8090"
	$(GO) run ./cmd/render $(ARGS)

run-voiceentry: ## 启动现场语音入口
	$(GO) run ./cmd/voiceentry -serve $(ARGS)

portal-dev: ## 启动 Portal 前端开发服务器
	cd $(PORTAL_DIR) && $(PNPM) dev

##@ 清理

clean: ## 删除构建产物（bin、coverage、portal/dist、嵌入目录）
	rm -rf bin coverage.out coverage.html coverage-*.out render-e2e-coverage.out $(PORTAL_DIR)/dist internal/gateway/portal/webdist
