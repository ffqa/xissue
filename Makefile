.DEFAULT_GOAL := help

BINARY   := bin/xissue
PORT     ?= 7789
DATA_DIR := $(HOME)/.xissue
LOG_FILE := $(DATA_DIR)/xissue.log

.PHONY: help build all web server deploy run dev stop restart status logs test clean \
        install-service uninstall-service service-status service-restart brew-publish

help: ## 显示此帮助菜单（默认）
	@printf "\033[1;36mxIssue (xpier-issue) 构建与服务管理菜单\033[0m\n\n"
	@printf "\033[1m用法:\033[0m\n  make \033[36m<target>\033[0m\n\n"
	@printf "\033[1m常用命令:\033[0m\n"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@printf "\n\033[1m服务地址:\033[0m http://127.0.0.1:$(PORT)  |  \033[1m数据目录:\033[0m $(DATA_DIR)\n"

all: build ## 同 build：完整构建前端与后端单文件二进制

build: web server ## 完整构建前端静态资源并内嵌打包为 bin/xissue

web: ## 仅类型检查并构建前端 (web -> internal/server/dist)
	./web/node_modules/.bin/tsc -p web/tsconfig.json --noEmit
	./web/node_modules/.bin/vite build web
	@mkdir -p internal/server/dist && touch internal/server/dist/.gitkeep

server: ## 仅编译 Go 后端二进制 (bin/xissue，使用已构建的前端产物)
	go build -o $(BINARY) ./cmd/xissue

deploy: build service-restart ## 一键编译前后端并重启后台常驻服务（开发改完代码最常用）
	@printf "\033[1;32m✓ 已完成构建并重启服务: http://127.0.0.1:$(PORT)\033[0m\n"

run: ## 前台直接启动已编译的 bin/xissue（若未编译则先构建）
	@test -x $(BINARY) || $(MAKE) build
	xattr -r -c ./$(BINARY) 2>/dev/null || true
	./$(BINARY)

dev: ## 前台使用 go run 启动后端服务
	go run ./cmd/xissue

install-service: build ## 构建并安装/启用系统级后台自启服务 (macOS launchd / Linux systemd)
	chmod +x ./scripts/install-service.sh ./$(BINARY)
	xattr -r -c ./$(BINARY) 2>/dev/null || true
	./scripts/install-service.sh install

mcp-install: server ## 一键将 xIssue MCP 插件写入本机所有 IDE / Agent 配置 (opencode / claude / cursor / windsurf)
	./$(BINARY) mcp install

uninstall-service: ## 停止并卸载系统级后台自启服务
	./scripts/install-service.sh uninstall

status: service-status ## 查看后台服务运行状态与端口监听情况

service-status: ## 查看后台服务 (launchd / systemd) 状态
	./scripts/install-service.sh status
	@lsof -i :$(PORT) || true

restart: service-restart ## 重启后台常驻服务（不重新构建）

service-restart: ## 重启后台常驻服务 (launchd / systemd)
	./scripts/install-service.sh restart

stop: ## 停止当前运行的后台服务或占用端口的进程
	./scripts/install-service.sh uninstall 2>/dev/null || true
	@lsof -ti tcp:$(PORT) | xargs kill -9 2>/dev/null || true
	@printf "已停止 xIssue 服务\n"

logs: ## 实时查看后台服务日志 (~/.xissue/xissue.log)
	tail -n 100 -f $(LOG_FILE)

test: ## 运行 Go 单元测试与前端 TypeScript 类型检查
	go test ./...
	./web/node_modules/.bin/tsc -p web/tsconfig.json --noEmit

clean: ## 清理编译产物 (bin/ 与 internal/server/dist/)
	rm -rf bin internal/server/dist

brew-publish: ## 发布最新版本 Formula 到 ffqa/homebrew-tap
	./scripts/brew-publish.sh

