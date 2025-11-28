.PHONY: help init dev build up down logs clean test dev-all \
        build-backend build-frontend build-debug build-release clean-dist \
        infra-up infra-down infra-restart infra-status ports-validate

# 默认目标
.DEFAULT_GOAL := help

# 颜色定义
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m # No Color

help: ## 显示帮助信息
	@echo "$(GREEN)全域数据平台 (ADDP) - Makefile 命令$(NC)"
	@echo ""
	@echo "$(YELLOW)可用命令:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""
	@echo "$(YELLOW)部署模式:$(NC)"
	@echo "  - System Only:  仅启动 System 模块（默认）"
	@echo "  - Full Platform: 启动所有模块 (使用 --profile full)"

# ===== 统一构建产物目录与变量 =====
# 统一输出目录：dist/{debug|release}/{backend|frontend}
OUT_DIR ?= dist
BUILD_TYPE ?= release
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BIN_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)

# 本地 Go 构建缓存目录，避免写入系统 GOPATH 并降低权限/网络问题
LOCAL_GOMODCACHE := $(abspath .gomodcache)
LOCAL_GOPATH := $(abspath .gopath)
LOCAL_GOCACHE := $(abspath .cache/go-build)
# 优先使用本机 Go 工具链，避免自动拉取 toolchain
GOTOOLCHAIN ?= local

# Go 编译参数：debug 保留符号，release 精简符号
GOFLAGS_DEBUG := -gcflags "all=-N -l"
GOFLAGS_RELEASE := -ldflags "-s -w"

# 内部函数：为指定服务编译到统一目录
define build_one_service
  @if [ -d $(1)/cmd ]; then \
    name=$(2); \
    outdir=$(OUT_DIR)/$(BUILD_TYPE)/backend/$$name/$(GOOS)-$(GOARCH); \
    mkdir -p $$outdir $(LOCAL_GOCACHE); \
    echo "$(GREEN)编译 $$name ($(BUILD_TYPE)) → $$outdir$(NC)"; \
    if [ "$(BUILD_TYPE)" = "debug" ]; then \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_DEBUG) -o ../../$$outdir/$$name$(BIN_SUFFIX) cmd/server/main.go 2>&1) || exit 1; \
    else \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o ../../$$outdir/$$name$(BIN_SUFFIX) cmd/server/main.go 2>&1) || exit 1; \
    fi; \
  else \
    true; \
  fi
endef

# 内部函数：为 Worker 服务编译到统一目录
define build_one_worker
  @if [ -d $(1)/cmd/worker ]; then \
    name=$(2)-worker; \
    outdir=$(OUT_DIR)/$(BUILD_TYPE)/backend/$$name/$(GOOS)-$(GOARCH); \
    mkdir -p $$outdir $(LOCAL_GOCACHE); \
    echo "$(GREEN)编译 $$name ($(BUILD_TYPE)) → $$outdir$(NC)"; \
    if [ "$(BUILD_TYPE)" = "debug" ]; then \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_DEBUG) -o ../../$$outdir/worker$(BIN_SUFFIX) cmd/worker/main.go 2>&1) || exit 1; \
    else \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o ../../$$outdir/worker$(BIN_SUFFIX) cmd/worker/main.go 2>&1) || exit 1; \
    fi; \
  else \
    true; \
  fi
endef

init: ## 初始化项目（创建必要的目录和配置文件）
	@echo "$(GREEN)初始化项目...$(NC)"
	@mkdir -p system/data
	@mkdir -p scripts
	@if [ ! -f .env ]; then cp .env.example .env && echo "$(GREEN)已创建 .env 文件$(NC)"; fi
	@echo "$(GREEN)初始化完成！$(NC)"

dev-system: ## 开发模式运行 System 模块
	@echo "$(GREEN)启动 System 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export POSTGRES_HOST=localhost; \
	  export REDIS_ADDR=localhost:6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
	  export ELASTICSEARCH_URL=$${ELASTICSEARCH_URL_LOCAL:-http://localhost:9200}; \
	  cd system/backend && go run cmd/server/main.go'

dev-manager: ## 开发模式运行 Manager 模块
	@echo "$(GREEN)启动 Manager 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export POSTGRES_HOST=localhost; \
	  export REDIS_ADDR=localhost:6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
	  export ELASTICSEARCH_URL=$${ELASTICSEARCH_URL_LOCAL:-http://localhost:9200}; \
	  cd manager/backend && go run cmd/server/main.go'

dev-meta: ## 开发模式运行 Meta 模块
	@echo "$(GREEN)启动 Meta 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export POSTGRES_HOST=localhost; \
	  export REDIS_ADDR=localhost:6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
	  export ELASTICSEARCH_URL=$${ELASTICSEARCH_URL_LOCAL:-http://localhost:9200}; \
	  cd meta/backend && go run cmd/server/main.go'

dev-transfer: ## 开发模式运行 Transfer 模块
	@echo "$(GREEN)启动 Transfer 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export REDIS_HOST=localhost; \
	  export REDIS_PORT=6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
	  export POSTGRES_HOST=localhost; \
	  export GOCACHE=$(abspath .cache/go-build); \
	  cd transfer/backend && go run cmd/server/main.go'

dev-gateway: ## 开发模式运行 Gateway 模块
	@echo "$(GREEN)启动 Gateway 模块开发环境...$(NC)"
	@cd gateway && go run cmd/gateway/main.go

dev-start: ## 开发模式启动所有服务（按正确顺序）
	@echo "$(GREEN)启动完整开发环境...$(NC)"
	@bash scripts/dev-start.sh

dev-stop: ## 停止所有开发模式服务
	@echo "$(YELLOW)停止开发环境...$(NC)"
	@bash scripts/dev-stop.sh

dev-all: ## 本地开发模式启动全部后端与前端服务
	@echo "$(GREEN)启动完整开发环境（Go + Vite）...$(NC)"
	@bash scripts/dev-run.sh

dev-health: ## 检查开发模式服务健康状态
	@echo "$(GREEN)检查服务健康状态...$(NC)"
	@curl -sf http://localhost:8080/health > /dev/null && echo "  $(GREEN)✓ System healthy$(NC)" || echo "  $(RED)✗ System unhealthy$(NC)"
	@curl -sf http://localhost:8081/health > /dev/null && echo "  $(GREEN)✓ Manager healthy$(NC)" || echo "  $(RED)✗ Manager unhealthy$(NC)"
	@curl -sf http://localhost:8082/health > /dev/null && echo "  $(GREEN)✓ Meta healthy$(NC)" || echo "  $(RED)✗ Meta unhealthy$(NC)"
	@curl -sf http://localhost:8000/health > /dev/null && echo "  $(GREEN)✓ Gateway healthy$(NC)" || echo "  $(RED)✗ Gateway unhealthy$(NC)"

build: build-release ## 编译所有服务（默认 release 输出到 dist）

# ===== 后端统一构建 =====
build-backend: ## 编译所有后端服务到 dist/{BUILD_TYPE}/backend
	@echo "$(GREEN)编译后端（$(BUILD_TYPE)）→ $(OUT_DIR)$(NC)"
	$(call build_one_service,system/backend,system)
	@if [ -d gateway/cmd ]; then \
	  outdir=$(OUT_DIR)/$(BUILD_TYPE)/backend/gateway/$(GOOS)-$(GOARCH); \
	  mkdir -p $$outdir; \
	  echo "$(GREEN)编译 gateway ($(BUILD_TYPE)) → $$outdir$(NC)"; \
	  if [ "$(BUILD_TYPE)" = "debug" ]; then \
	    (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOTOOLCHAIN=$(GOTOOLCHAIN) \
	     cd gateway && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_DEBUG) -o ../$$outdir/gateway$(BIN_SUFFIX) cmd/gateway/main.go); \
	  else \
	    (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOTOOLCHAIN=$(GOTOOLCHAIN) \
	     cd gateway && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o ../$$outdir/gateway$(BIN_SUFFIX) cmd/gateway/main.go); \
	  fi; \
	fi
	$(call build_one_service,manager/backend,manager)
	$(call build_one_service,meta/backend,meta)
	$(call build_one_service,transfer/backend,transfer)
	@echo "$(GREEN)后端编译完成！$(NC)"

# ===== Worker 构建 =====
build-workers: ## 编译所有 Worker 服务到 dist/{BUILD_TYPE}/backend
	@echo "$(GREEN)编译 Worker 服务（$(BUILD_TYPE)）→ $(OUT_DIR)$(NC)"
	$(call build_one_worker,transfer/backend,transfer)
	$(call build_one_worker,meta/backend,meta)
	@echo "$(GREEN)Worker 编译完成！$(NC)"

# ===== 前端统一构建 =====
build-frontend: ## 编译所有前端到 dist/{BUILD_TYPE}/frontend/{system|portal}
	@echo "$(GREEN)编译前端（$(BUILD_TYPE)）→ $(OUT_DIR)$(NC)"
	@if [ -f system/frontend/package.json ]; then \
	  echo "  - system/frontend"; \
	  if [ "$(BUILD_TYPE)" = "debug" ]; then \
	    (cd system/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent -- --mode development); \
	  else \
	    (cd system/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent); \
	  fi; \
	fi
	@if [ -f portal/frontend/package.json ]; then \
	  echo "  - portal/frontend"; \
	  if [ "$(BUILD_TYPE)" = "debug" ]; then \
	    (cd portal/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent -- --mode development); \
	  else \
	    (cd portal/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent); \
	  fi; \
	fi
	@echo "$(GREEN)前端编译完成！$(NC)"

# 便捷目标
build-debug: ## 构建 debug（后端 + 前端 + workers）输出到 dist/debug
	@$(MAKE) BUILD_TYPE=debug build-backend
	@$(MAKE) BUILD_TYPE=debug build-workers
	@$(MAKE) BUILD_TYPE=debug build-frontend

build-release: ## 构建 release（后端 + 前端 + workers）输出到 dist/release
	@$(MAKE) BUILD_TYPE=release build-backend
	@$(MAKE) BUILD_TYPE=release build-workers
	@$(MAKE) BUILD_TYPE=release build-frontend

clean-dist: ## 清理 dist 构建产物
	@rm -rf $(OUT_DIR)
	@echo "$(YELLOW)已清理 $(OUT_DIR)$(NC)"

docker-build: ## 构建 Docker 镜像（仅 System 模块）
	@echo "$(GREEN)构建 System 模块 Docker 镜像...$(NC)"
	@docker compose build system-backend system-frontend
	@echo "$(GREEN)构建完成！$(NC)"

docker-build-all: ## 构建所有服务的 Docker 镜像
	@echo "$(GREEN)构建所有服务的 Docker 镜像...$(NC)"
	@docker compose --profile full build
	@echo "$(GREEN)所有镜像构建完成！$(NC)"

up: ## 启动 System 模块（基础服务）
	@echo "$(GREEN)启动 System 模块...$(NC)"
	@docker compose up -d system-backend system-frontend
	@echo "$(GREEN)System 模块已启动！$(NC)"
	@echo "$(YELLOW)访问地址:$(NC)"
	@echo "  - System Backend:  http://localhost:8080"
	@echo "  - System Frontend: http://localhost:8090"

up-full: ## 启动所有服务（完整平台）
	@echo "$(GREEN)启动完整平台（所有服务）...$(NC)"
	@docker compose --profile full up -d
	@echo "$(GREEN)所有服务已启动！$(NC)"
	@$(MAKE) status

up-infra: ## 仅启动基础设施服务（PostgreSQL, Redis, MinIO, Elasticsearch）
	@echo "$(GREEN)启动基础设施服务...$(NC)"
	@docker compose up -d postgres redis minio elasticsearch
	@echo "$(GREEN)基础设施服务已启动！$(NC)"

down: ## 停止所有服务
	@echo "$(YELLOW)停止所有服务...$(NC)"
	@docker compose --profile full down
	@echo "$(GREEN)所有服务已停止$(NC)"

restart: down up ## 重启 System 模块

restart-full: down up-full ## 重启所有服务

logs: ## 查看所有服务日志
	@docker compose --profile full logs -f

logs-system: ## 查看 System 模块日志
	@docker compose logs -f system-backend system-frontend

logs-manager: ## 查看 Manager 模块日志
	@docker compose logs -f manager-backend

logs-meta: ## 查看 Meta 模块日志
	@docker compose logs -f meta-backend

logs-transfer: ## 查看 Transfer 模块日志
	@docker compose logs -f transfer-backend transfer-worker

logs-gateway: ## 查看 Gateway 模块日志
	@docker compose logs -f gateway

status: ## 显示所有服务状态
	@echo "$(GREEN)服务状态:$(NC)"
	@docker compose --profile full ps
	@echo ""
	@echo "$(YELLOW)服务访问地址:$(NC)"
	@echo "  - Gateway:          http://localhost:8000  (未实现)"
	@echo "  - System Backend:   http://localhost:8080"
	@echo "  - System Frontend:  http://localhost:8090"
	@echo "  - Manager Backend:  http://localhost:8081  (未实现)"
	@echo "  - Manager Frontend: http://localhost:8091  (未实现)"
	@echo "  - Meta Backend:     http://localhost:8082  (未实现)"
	@echo "  - Meta Frontend:    http://localhost:8092  (未实现)"
	@echo "  - Transfer Backend: http://localhost:8083  (未实现)"
	@echo "  - Transfer Frontend:http://localhost:8093  (未实现)"
	@echo ""
	@echo "$(YELLOW)基础设施服务:$(NC)"
	@echo "  - PostgreSQL:       localhost:5432"
	@echo "  - Redis:            localhost:6379"
	@echo "  - MinIO Console:    http://localhost:9003"
	@echo "  - MinIO API:        http://localhost:9002"
	@echo "  - Elasticsearch:    http://localhost:9200"

# ===== 基础设施脚本别名 =====
infra-up: ## 启动系统库基础设施（带端口预检与健康检查）
	@bash scripts/infra-up.sh

infra-down: ## 停止系统库基础设施（可选 --rm） 用法：make infra-down ARGS=--rm
	@bash scripts/infra-down.sh $(ARGS)

infra-restart: ## 重启系统库基础设施（先停再启）
	@bash scripts/infra-down.sh || true
	@bash scripts/infra-up.sh

infra-status: ## 查看系统库基础设施状态与健康
	@bash scripts/infra-status.sh

ports-validate: ## 校验 System/Business 端口分配是否符合策略
	@bash scripts/ports-validate.sh

ps: status ## 显示服务状态（别名）

clean: ## 清理编译产物和临时文件
	@echo "$(YELLOW)清理编译产物...$(NC)"
	@rm -rf bin/
	@rm -rf system/backend/server
	@rm -rf system/frontend/dist
	@cd system && $(MAKE) clean
	@echo "$(GREEN)清理完成$(NC)"

clean-all: clean ## 清理所有数据（包括 Docker volumes 和数据库）
	@echo "$(RED)警告: 此操作将删除所有数据！$(NC)"
	@read -p "确认删除所有数据？(yes/no): " confirm; \
	if [ "$$confirm" = "yes" ]; then \
		docker compose --profile full down -v; \
		rm -rf system/data/*.db; \
		echo "$(GREEN)所有数据已清理$(NC)"; \
	else \
		echo "$(YELLOW)操作已取消$(NC)"; \
	fi

test: ## 运行所有测试
	@echo "$(GREEN)运行测试...$(NC)"
	@cd system/backend && go test ./...
	@if [ -d manager/backend ]; then cd manager/backend && go test ./...; fi
	@if [ -d meta/backend ]; then cd meta/backend && go test ./...; fi
	@if [ -d transfer/backend ]; then cd transfer/backend && go test ./...; fi
	@echo "$(GREEN)所有测试完成$(NC)"

test-system: ## 运行 System 模块测试
	@cd system/backend && go test ./...

db-migrate: ## 运行数据库迁移（重新初始化数据库）
	@echo "$(GREEN)运行数据库迁移...$(NC)"
	@docker compose exec -T postgres psql -U addp -d addp < scripts/init-db.sql
	@echo "$(GREEN)数据库迁移完成$(NC)"

db-shell: ## 连接到 PostgreSQL 数据库
	@docker compose exec postgres psql -U addp -d addp

redis-cli: ## 连接到 Redis
	@docker compose exec redis redis-cli -a addp_redis

minio-setup: ## 初始化 MinIO bucket (legacy, 使用 init-minio 替代)
	@echo "$(GREEN)初始化 MinIO...$(NC)"
	@docker compose exec minio mc alias set local http://localhost:9000 minioadmin minioadmin
	@docker compose exec minio mc mb local/addp-data --ignore-existing
	@echo "$(GREEN)MinIO 初始化完成$(NC)"

init-minio: ## 初始化 MinIO buckets (包括 mvt-tiles 等)
	@./scripts/infra-init-minio.sh

init-minio-mvt: init-minio ## 初始化 MVT 瓦片缓存 bucket (alias for init-minio)

init-redis: ## 初始化 Redis 任务队列和缓存配置
	@./scripts/infra-init-redis.sh

install-deps: ## 安装所有依赖
	@echo "$(GREEN)安装依赖...$(NC)"
	@cd system/backend && go mod download
	@cd system/frontend && npm install
	@echo "$(GREEN)依赖安装完成$(NC)"

update-deps: ## 更新所有依赖
	@echo "$(GREEN)更新依赖...$(NC)"
	@cd system/backend && go get -u ./...
	@cd system/frontend && npm update
	@echo "$(GREEN)依赖更新完成$(NC)"

lint: ## 运行代码检查
	@echo "$(GREEN)运行代码检查...$(NC)"
	@cd system/backend && golangci-lint run || echo "$(YELLOW)请安装 golangci-lint$(NC)"
	@cd system/frontend && npm run lint || echo "$(YELLOW)前端 lint 未配置$(NC)"

fmt: ## 格式化代码
	@echo "$(GREEN)格式化代码...$(NC)"
	@find . -name "*.go" -not -path "*/vendor/*" -not -path "*/node_modules/*" -exec gofmt -w {} \;
	@echo "$(GREEN)代码格式化完成$(NC)"

health: ## 检查所有服务健康状态
	@echo "$(GREEN)检查服务健康状态...$(NC)"
	@echo "System Backend:"
	@curl -s http://localhost:8080/health || echo "$(RED)  ✗ 不可用$(NC)"
	@echo ""
	@echo "PostgreSQL:"
	@docker compose exec postgres pg_isready -U addp > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"
	@echo "Redis:"
	@docker compose exec redis redis-cli -a addp_redis ping > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"
	@echo "MinIO:"
	@curl -s http://localhost:9002/minio/health/live > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"

backup: ## 备份数据库
	@echo "$(GREEN)备份数据库...$(NC)"
	@mkdir -p backups
	@docker compose exec -T postgres pg_dump -U addp addp > backups/addp_$(shell date +%Y%m%d_%H%M%S).sql
	@echo "$(GREEN)数据库备份完成$(NC)"

restore: ## 恢复数据库（需要指定备份文件 FILE=xxx.sql）
	@if [ -z "$(FILE)" ]; then \
		echo "$(RED)错误: 请指定备份文件 FILE=xxx.sql$(NC)"; \
		exit 1; \
	fi
	@echo "$(YELLOW)恢复数据库: $(FILE)$(NC)"
	@docker compose exec -T postgres psql -U addp -d addp < $(FILE)
	@echo "$(GREEN)数据库恢复完成$(NC)"

.PHONY: docs
docs: ## 生成 API 文档
	@echo "$(GREEN)生成 API 文档...$(NC)"
	@echo "$(YELLOW)TODO: 实现 API 文档生成$(NC)"

# ==================== 生产环境命令 ====================

prod-start: ## 启动生产环境（一键启动）
	@./scripts/start-prod.sh

prod-stop: ## 停止生产环境
	@./scripts/stop-prod.sh

prod-restart: ## 重启生产环境
	@./scripts/stop-prod.sh
	@./scripts/start-prod.sh

prod-logs: ## 查看生产环境日志
	@docker compose -f docker-compose.prod.yml logs -f

prod-status: ## 查看生产环境状态
	@docker compose -f docker-compose.prod.yml ps
