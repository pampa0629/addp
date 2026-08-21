.PHONY: help init dev build up down logs clean test test-platform test-online test-online-runner test-go test-asset-frontend test-console-frontend test-develop-frontend test-graph-frontend test-inference-frontend test-manager-frontend test-model-frontend test-quality-frontend test-meta-frontend test-monitor-frontend test-orchestrator-frontend test-portal-frontend test-service-frontend test-standard-frontend test-system-frontend test-transfer-frontend test-execution-fixtures test-authorization authorization-generate test-agent-eval test-agent-eval-release compare-agent-eval compare-agent-eval-release test-common-python test-common-python-cli-release test-system-iam-postgres test-quality-postgres test-standard-postgres test-arcgis-open-formats dev-all \
        build-backend build-frontend build-debug build-release build-iam-bootstrap build-iam-recovery clean-dist \
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
# 扁平化输出目录：dist/{type}-{build}-{os}-{arch}/
OUT_DIR ?= dist
BUILD_TYPE ?= release
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BIN_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)

# 多架构编译支持
MULTI_ARCHS := amd64 arm64

# 本地 Go 构建缓存目录，避免写入系统 GOPATH 并降低权限/网络问题
LOCAL_GOMODCACHE := $(abspath .gomodcache)
LOCAL_GOPATH := $(abspath .gopath)
LOCAL_GOCACHE := $(abspath .cache/go-build)
# 优先使用本机 Go 工具链，避免自动拉取 toolchain
GOTOOLCHAIN ?= local

# Go 编译参数：debug 保留符号，release 精简符号
GOFLAGS_DEBUG := -gcflags "all=-N -l"
GOFLAGS_RELEASE := -ldflags "-s -w"

# 内部函数：为指定服务编译到统一目录（扁平化结构）
# 参数: $(1)=模块目录, $(2)=二进制名称, $(3)=cmd路径(可选,默认cmd/server/main.go)
define build_one_service
  @if [ -d $(1)/cmd ]; then \
    name=$(2); \
    cmd_path=$(if $(3),$(3),cmd/server/main.go); \
    outdir=$(CURDIR)/$(OUT_DIR)/$(BUILD_TYPE)-$(GOOS)-$(GOARCH); \
    mkdir -p $$outdir $(LOCAL_GOCACHE); \
    echo "$(GREEN)编译 $$name ($(BUILD_TYPE)) → $$outdir/$$name$(NC)"; \
    if [ "$(BUILD_TYPE)" = "debug" ]; then \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_DEBUG) -o $$outdir/$$name$(BIN_SUFFIX) $$cmd_path 2>&1) || exit 1; \
    else \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o $$outdir/$$name$(BIN_SUFFIX) $$cmd_path 2>&1) || exit 1; \
    fi; \
  else \
    true; \
  fi
endef

# 内部函数：为 Worker 服务编译到统一目录（与 backend 合并）
define build_one_worker
  @if [ -d $(1)/cmd/worker ]; then \
    name=$(2)-worker; \
    outdir=$(CURDIR)/$(OUT_DIR)/$(BUILD_TYPE)-$(GOOS)-$(GOARCH); \
    mkdir -p $$outdir $(LOCAL_GOCACHE); \
    echo "$(GREEN)编译 $$name ($(BUILD_TYPE)) → $$outdir/$$name$(NC)"; \
    if [ "$(BUILD_TYPE)" = "debug" ]; then \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_DEBUG) -o $$outdir/$$name$(BIN_SUFFIX) cmd/worker/main.go 2>&1) || exit 1; \
    else \
      (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
       cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o $$outdir/$$name$(BIN_SUFFIX) cmd/worker/main.go 2>&1) || exit 1; \
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
	  cd system/backend && go run cmd/server/main.go'

build-iam-bootstrap: ## 构建一次性离线 IAM Bootstrap CLI
	$(call build_one_service,system/backend,addp-iam-bootstrap,cmd/iam-bootstrap/main.go)

build-iam-recovery: ## 构建离线 IAM 三员凭据恢复 CLI
	$(call build_one_service,system/backend,addp-iam-recovery,cmd/iam-recovery/main.go)

dev-manager: ## 开发模式运行 Manager 模块
	@echo "$(GREEN)启动 Manager 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export POSTGRES_HOST=localhost; \
	  export REDIS_ADDR=localhost:6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
	  cd manager/backend && go run cmd/server/main.go'

dev-meta: ## 开发模式运行 Meta 模块
	@echo "$(GREEN)启动 Meta 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export POSTGRES_HOST=localhost; \
	  export REDIS_ADDR=localhost:6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
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

dev-orchestrator: ## 开发模式运行 Orchestrator 模块
	@echo "$(GREEN)启动 Orchestrator 模块开发环境...$(NC)"
	@bash -c 'set -a; [ -f .env ] && source .env; set +a; \
	  export REDIS_HOST=localhost; \
	  export REDIS_PORT=6379; \
	  export REDIS_PASSWORD=$${REDIS_PASSWORD:-addp_redis}; \
	  export POSTGRES_HOST=localhost; \
	  export GOCACHE=$(abspath .cache/go-build); \
	  cd orchestrator/backend && go run cmd/server/main.go'

dev-gateway: ## 开发模式运行 Gateway 模块
	@echo "$(GREEN)启动 Gateway 模块开发环境...$(NC)"
	@cd gateway && go run cmd/gateway/main.go

dev-geopython-workflow: ## 开发模式运行 GeoPython Workflow
	@echo "$(GREEN)启动 GeoPython Workflow 开发环境...$(NC)"
	@cd engines/geopython-workflow && \
	if [ ! -d "venv" ]; then \
		echo "创建 Python 虚拟环境..." && \
		python3 -m venv venv; \
	fi && \
	if ! ./venv/bin/python -c "import flask, geopandas, pyarrow, pyproj" >/dev/null 2>&1; then \
		echo "检测到虚拟环境缺少依赖，重新安装..." && \
		./venv/bin/python -m pip install --quiet -r requirements.txt; \
	fi && \
	export PORT=8099 && \
	export SYSTEM_URL=http://localhost:8180 && \
	export POSTGRES_HOST=localhost && \
	export POSTGRES_PORT=5432 && \
	export POSTGRES_USER=addp && \
	export POSTGRES_PASSWORD=addp_password && \
	export POSTGRES_DB=addp && \
	export DB_SCHEMA=develop && \
	./venv/bin/python api_server.py

dev-start: ## 开发模式启动所有服务（按正确顺序）
	@echo "$(GREEN)启动完整开发环境...$(NC)"
	@bash scripts/dev/start.sh

dev-stop: ## 停止所有开发模式服务
	@echo "$(YELLOW)停止开发环境...$(NC)"
	@bash scripts/dev/stop.sh

dev-all: ## 本地开发模式启动全部后端与前端服务
	@echo "$(GREEN)启动完整开发环境（Go + Vite）...$(NC)"
	@bash scripts/dev/run.sh

dev-health: ## 检查开发模式服务健康状态
	@echo "$(GREEN)检查服务健康状态...$(NC)"
	@curl -sf http://localhost:8180/health > /dev/null && echo "  $(GREEN)✓ System healthy$(NC)" || echo "  $(RED)✗ System unhealthy$(NC)"
	@curl -sf http://localhost:8081/health > /dev/null && echo "  $(GREEN)✓ Manager healthy$(NC)" || echo "  $(RED)✗ Manager unhealthy$(NC)"
	@curl -sf http://localhost:8082/health > /dev/null && echo "  $(GREEN)✓ Meta healthy$(NC)" || echo "  $(RED)✗ Meta unhealthy$(NC)"
	@curl -sf http://localhost:8099/health > /dev/null && echo "  $(GREEN)✓ GeoPython Workflow healthy$(NC)" || echo "  $(RED)✗ GeoPython Workflow unhealthy$(NC)"
	@curl -sf http://localhost:8000/health > /dev/null && echo "  $(GREEN)✓ Gateway healthy$(NC)" || echo "  $(RED)✗ Gateway unhealthy$(NC)"

build: build-release ## 编译所有服务（默认 release 输出到 dist）

# ===== 后端统一构建 =====
build-backend: ## 编译所有后端服务到 dist/{BUILD_TYPE}-{GOOS}-{GOARCH}/
	@echo "$(GREEN)编译后端（$(BUILD_TYPE)）→ $(OUT_DIR)$(NC)"
	$(call build_one_service,system/backend,system)
	$(call build_one_service,gateway,gateway,cmd/gateway/main.go)
	$(call build_one_service,manager/backend,manager)
	$(call build_one_service,meta/backend,meta)
	$(call build_one_service,transfer/backend,transfer)
	@echo "$(GREEN)后端编译完成！$(NC)"

# ===== Worker 构建 (合并到同一目录) =====
build-workers: ## 编译所有 Worker 到 dist/{BUILD_TYPE}-{GOOS}-{GOARCH}/
	@echo "$(GREEN)编译 Worker 服务（$(BUILD_TYPE)）→ $(OUT_DIR)$(NC)"
	$(call build_one_worker,transfer/backend,transfer)
	$(call build_one_worker,meta/backend,meta)
	@echo "$(GREEN)Worker 编译完成！$(NC)"

# ===== 多架构编译 =====
build-backend-multiarch: ## 编译所有后端服务（amd64 + arm64）
	@echo "$(GREEN)编译后端（多架构: $(MULTI_ARCHS)）→ $(OUT_DIR)$(NC)"
	@for arch in $(MULTI_ARCHS); do \
		echo "$(YELLOW)编译架构: $$arch$(NC)"; \
		$(MAKE) GOARCH=$$arch build-backend; \
	done
	@echo "$(GREEN)多架构后端编译完成！$(NC)"

build-workers-multiarch: ## 编译所有 Worker 服务（amd64 + arm64）
	@echo "$(GREEN)编译 Worker（多架构: $(MULTI_ARCHS)）→ $(OUT_DIR)$(NC)"
	@for arch in $(MULTI_ARCHS); do \
		echo "$(YELLOW)编译架构: $$arch$(NC)"; \
		$(MAKE) GOARCH=$$arch build-workers; \
	done
	@echo "$(GREEN)多架构 Worker 编译完成！$(NC)"

build-backend-all-multiarch: build-backend-multiarch build-workers-multiarch ## 编译所有服务（后端 + Worker，多架构）

build-backend-all-local: ## 编译所有服务（仅当前架构，快速）
	@echo "$(GREEN)编译所有服务（当前架构: $(GOARCH)）→ $(OUT_DIR)$(NC)"
	@$(MAKE) build-backend build-workers
	@echo "$(GREEN)本地架构编译完成！$(NC)"

# ===== 前端统一构建 =====
build-frontend: ## 编译所有前端到 dist/{BUILD_TYPE}/frontend/{system|console}
	@echo "$(GREEN)编译前端（$(BUILD_TYPE)）→ $(OUT_DIR)$(NC)"
	@if [ -f system/frontend/package.json ]; then \
	  echo "  - system/frontend"; \
	  if [ "$(BUILD_TYPE)" = "debug" ]; then \
	    (cd system/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent -- --mode development); \
	  else \
	    (cd system/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent); \
	  fi; \
	fi
	@if [ -f console/frontend/package.json ]; then \
	  echo "  - console/frontend"; \
	  if [ "$(BUILD_TYPE)" = "debug" ]; then \
	    (cd console/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent -- --mode development); \
	  else \
	    (cd console/frontend && BUILD_TYPE=$(BUILD_TYPE) OUT_DIR=../../$(OUT_DIR) npm run build --silent); \
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

# ==================== 生产环境本地编译 ====================

build-backends: ## 编译所有后端服务到 dist/ 目录（用于生产镜像）
	@echo "$(GREEN)编译所有后端服务（Linux AMD64）...$(NC)"
	@mkdir -p dist
	@echo "$(YELLOW)编译 system-backend...$(NC)"
	@cd system/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/system-backend ./cmd/server
	@echo "$(YELLOW)编译 manager-backend...$(NC)"
	@cd manager/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/manager-backend ./cmd/server
	@echo "$(YELLOW)编译 meta-backend...$(NC)"
	@cd meta/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/meta-backend ./cmd/server
	@echo "$(YELLOW)编译 meta-worker...$(NC)"
	@cd meta/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/meta-worker ./cmd/worker
	@echo "$(YELLOW)编译 transfer-backend...$(NC)"
	@cd transfer/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/transfer-backend ./cmd/server
	@echo "$(YELLOW)编译 transfer-bounded-worker...$(NC)"
	@cd transfer/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/transfer-bounded-worker ./cmd/worker
	@echo "$(YELLOW)编译 transfer-continuous-worker...$(NC)"
	@cd transfer/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/transfer-continuous-worker ./cmd/continuous-worker
	@echo "$(YELLOW)编译 quality-worker...$(NC)"
	@cd quality/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/quality-worker ./cmd/worker
	@echo "$(YELLOW)编译 orchestrator-backend...$(NC)"
	@cd orchestrator/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/orchestrator-backend ./cmd/server
	@echo "$(YELLOW)编译 develop-backend...$(NC)"
	@cd develop/backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../../dist/develop-backend ./cmd/server
	@echo "$(YELLOW)编译 gateway...$(NC)"
	@cd gateway && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../dist/gateway ./cmd/gateway
	@echo "$(GREEN)所有后端编译完成！$(NC)"
	@ls -lh dist/

prod-build-images: build-backends ## 构建所有生产 Docker 镜像（使用预编译二进制）
	@echo "$(GREEN)构建所有生产镜像（从项目根目录）...$(NC)"
	@docker build -t localhost:5001/addp-system-backend:latest -f system/backend/Dockerfile .
	@docker build -t localhost:5001/addp-manager-backend:latest -f manager/backend/Dockerfile .
	@docker build -t localhost:5001/addp-meta-backend:latest -f meta/backend/Dockerfile .
	@docker build -t localhost:5001/addp-meta-worker:latest -f meta/worker/Dockerfile .
	@docker build -t localhost:5001/addp-transfer-backend:latest -f transfer/backend/Dockerfile .
	@docker build -t localhost:5001/addp-transfer-bounded-worker:latest -f transfer/worker/Dockerfile .
	@docker build -t localhost:5001/addp-transfer-continuous-worker:latest -f transfer/continuous-worker/Dockerfile .
	@docker build -t localhost:5001/addp-quality-worker:latest -f quality/worker/Dockerfile .
	@docker build -t localhost:5001/addp-orchestrator-backend:latest -f orchestrator/backend/Dockerfile .
	@docker build -t localhost:5001/addp-develop-backend:latest -f develop/backend/Dockerfile .
	@docker build -t localhost:5001/addp-gateway:latest -f gateway/Dockerfile .
	@echo "$(GREEN)所有后端镜像构建完成！$(NC)"
	@echo "$(YELLOW)提示：前端镜像需要在各自目录单独构建$(NC)"

docker-build: ## 构建 Docker 镜像（仅 System 模块）
	@echo "$(GREEN)构建 System 模块 Docker 镜像...$(NC)"
	@docker compose -f docker-compose.yml build system-backend system-frontend
	@echo "$(GREEN)构建完成！$(NC)"

docker-build-all: ## 构建所有服务的 Docker 镜像
	@echo "$(GREEN)构建所有服务的 Docker 镜像...$(NC)"
	@docker compose -f docker-compose.yml --profile full build
	@echo "$(GREEN)所有镜像构建完成！$(NC)"

up: ## 启动 System 模块（基础服务）
	@echo "$(GREEN)启动 System 模块...$(NC)"
	@docker compose -f docker-compose.yml up -d system-backend system-frontend
	@echo "$(GREEN)System 模块已启动！$(NC)"
	@echo "$(YELLOW)访问地址:$(NC)"
	@echo "  - System Backend:  http://localhost:8180"
	@echo "  - System Frontend: http://localhost:8090"

up-full: ## 启动所有服务（完整平台）
	@echo "$(GREEN)启动完整平台（所有服务）...$(NC)"
	@docker compose -f docker-compose.yml --profile full up -d
	@echo "$(GREEN)所有服务已启动！$(NC)"
	@$(MAKE) status

up-infra: ## 仅启动基础设施服务（PostgreSQL, Redis, MinIO, Meilisearch）
	@echo "$(GREEN)启动基础设施服务...$(NC)"
	@docker compose -f docker-compose.infra.yml up -d
	@echo "$(GREEN)基础设施服务已启动！$(NC)"

down: ## 停止所有服务
	@echo "$(YELLOW)停止所有服务...$(NC)"
	@docker compose -f docker-compose.yml --profile full down
	@echo "$(GREEN)所有服务已停止$(NC)"

restart: down up ## 重启 System 模块

restart-full: down up-full ## 重启所有服务

logs: ## 查看所有服务日志
	@docker compose -f docker-compose.yml --profile full logs -f

logs-system: ## 查看 System 模块日志
	@docker compose -f docker-compose.yml logs -f system-backend system-frontend

logs-manager: ## 查看 Manager 模块日志
	@docker compose -f docker-compose.yml logs -f manager-backend

logs-meta: ## 查看 Meta 模块日志
	@docker compose -f docker-compose.yml logs -f meta-backend

logs-transfer: ## 查看 Transfer 模块日志
	@docker compose -f docker-compose.yml logs -f transfer-backend transfer-bounded-worker transfer-continuous-worker

logs-orchestrator: ## 查看 Orchestrator 模块日志
	@docker compose -f docker-compose.yml logs -f orchestrator-backend orchestrator-frontend

logs-gateway: ## 查看 Gateway 模块日志
	@docker compose -f docker-compose.yml logs -f gateway

status: ## 显示所有服务状态
	@echo "$(GREEN)服务状态:$(NC)"
	@docker compose -f docker-compose.yml --profile full ps
	@echo ""
	@echo "$(YELLOW)服务访问地址:$(NC)"
	@echo "  - Gateway:          http://localhost:8000  (未实现)"
	@echo "  - System Backend:   http://localhost:8180"
	@echo "  - System Frontend:  http://localhost:8090"
	@echo "  - Manager Backend:  http://localhost:8081  (未实现)"
	@echo "  - Manager Frontend: http://localhost:8091  (未实现)"
	@echo "  - Meta Backend:     http://localhost:8082  (未实现)"
	@echo "  - Meta Frontend:    http://localhost:8092  (未实现)"
	@echo "  - Transfer Backend: http://localhost:8083  (未实现)"
	@echo "  - Transfer Frontend:http://localhost:8093  (未实现)"
	@echo "  - Orchestrator Backend: http://localhost:8084"
	@echo "  - Orchestrator Frontend:http://localhost:8094"
	@echo ""
	@echo "$(YELLOW)基础设施服务:$(NC)"
	@echo "  - PostgreSQL:       localhost:5432"
	@echo "  - Redis:            localhost:6379"
	@echo "  - MinIO Console:    http://localhost:9001"
	@echo "  - MinIO API:        http://localhost:9000"

# ===== 基础设施脚本别名 =====
infra-up: ## 启动系统库基础设施（带端口预检与健康检查）
	@bash scripts/infra/up.sh

infra-down: ## 停止系统库基础设施（可选 --rm） 用法：make infra-down ARGS=--rm
	@bash scripts/infra/down.sh $(ARGS)

infra-restart: ## 重启系统库基础设施（先停再启）
	@bash scripts/infra/down.sh || true
	@bash scripts/infra/up.sh

infra-status: ## 查看系统库基础设施状态与健康
	@bash scripts/infra/status.sh

ports-validate: ## 校验 System/Business 端口分配是否符合策略
	@bash scripts/utils/ports-validate.sh

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
		docker compose -f docker-compose.yml --profile full down -v; \
		docker compose -f docker-compose.infra.yml down -v; \
		rm -rf system/data/*.db; \
		echo "$(GREEN)所有数据已清理$(NC)"; \
	else \
		echo "$(YELLOW)操作已取消$(NC)"; \
	fi

test-agent-eval: ## 运行 Agent 统一离线评测门禁
	@bash scripts/test/agent-evaluation-gate.sh offline

test-agent-eval-release: ## 使用三份新鲜在线证据运行 Agent 发布门禁
	@bash scripts/test/agent-evaluation-gate.sh release

test-common-python: ## 运行 common-python 全量测试
	@cd common-python && .venv/bin/pytest -q

test-common-python-cli-release: ## 构建 wheel 并运行全新 venv、pipx 生命周期和 macOS Keychain 发布门禁
	@bash scripts/test/common-python-cli-release-gate.sh

test-system-iam-postgres: ## 使用一次性 PostgreSQL 数据库运行 System IAM 发布门禁
	@bash scripts/test/system-iam-postgres-gate.sh

test-quality-postgres: ## 使用一次性 PostgreSQL 数据库运行 Quality 集成门禁
	@bash scripts/test/quality-postgres-gate.sh

test-standard-postgres: ## 使用一次性 PostgreSQL 数据库运行 Standard 集成门禁
	@bash scripts/test/standard-postgres-gate.sh

test-arcgis-open-formats: ## 使用真实 Access/PGeo 样本和 Oracle Spatial 运行集成门禁
	@bash scripts/test/arcgis-open-formats-integration-gate.sh

test-execution-fixtures: ## 校验统一执行存储测试夹具
	@bash scripts/test/check-execution-test-fixtures.sh

test-online: ## 运行指定 Online suite（必须设置 ONLINE_SUITE 和 ADDP_ONLINE_TEST=1）
	@test -n "$(ONLINE_SUITE)" || (echo "ONLINE_SUITE is required" >&2; exit 2)
	@python3 scripts/test/online-gate.py --repository "$(CURDIR)" --suite "$(ONLINE_SUITE)"

test-online-runner: ## 运行 Online 分发器和预检器的确定性测试
	@python3 -m unittest scripts/test/online-gate_test.py scripts/test/online-preflight_test.py scripts/test/standard-model-reference-deletion-online_test.py

test-platform: ## 运行无外部服务依赖的平台一致性门禁
	@bash scripts/utils/check-deps-version.sh
	@$(MAKE) test-execution-fixtures
	@$(MAKE) test-online-runner
	@$(MAKE) test-authorization

test-model-frontend: ## 运行 Model 前端状态、交互与浏览器回归测试
	@cd model/frontend && npm test
	@cd model/frontend && npm run test:e2e
	@cd model/frontend && npm run build

test-quality-frontend: ## 运行 Quality 前端路由、浏览器与构建门禁
	@cd quality/frontend && npm run test:route
	@cd quality/frontend && npm run test:e2e
	@cd quality/frontend && npm run build

test-asset-frontend: ## 运行 Asset 前端确定性测试与构建
	@cd asset/frontend && npm test
	@cd asset/frontend && npm run build

test-console-frontend: ## 运行 Console 前端确定性测试与构建
	@cd console/frontend && npm test
	@cd console/frontend && npm run build

test-develop-frontend: ## 运行 Develop 前端确定性测试与构建
	@cd develop/frontend && npm run test:workflow
	@cd develop/frontend && npm run build

test-graph-frontend: ## 运行 Graph 前端确定性测试与构建
	@cd graph/frontend && npm test
	@cd graph/frontend && npm run build

test-inference-frontend: ## 运行 Inference 前端确定性测试与构建
	@cd inference/frontend && npm test
	@cd inference/frontend && npm run build

test-manager-frontend: ## 运行 Manager 前端确定性测试与构建
	@cd manager/frontend && npm test
	@cd manager/frontend && npm run build

test-meta-frontend: ## 运行 Meta 前端确定性测试与构建
	@cd meta/frontend && npm test
	@cd meta/frontend && npm run build

test-monitor-frontend: ## 运行 Monitor 前端确定性测试与构建
	@cd monitor/frontend && npm test
	@cd monitor/frontend && npm run build

test-orchestrator-frontend: ## 运行 Orchestrator 前端确定性测试与构建
	@cd orchestrator/frontend && npm test
	@cd orchestrator/frontend && npm run build

test-portal-frontend: ## 运行 Portal 前端确定性测试与构建
	@cd portal/frontend && npm test
	@cd portal/frontend && npm run build

test-service-frontend: ## 运行 Service 前端确定性测试与构建
	@cd service/frontend && npm test
	@cd service/frontend && npm run build

test-standard-frontend: ## 运行 Standard 前端确定性测试与构建
	@cd standard/frontend && npm test
	@cd standard/frontend && npm run build

test-system-frontend: ## 运行 System 前端确定性测试与构建
	@cd system/frontend && npm test
	@cd system/frontend && npm run build

test-transfer-frontend: ## 运行 Transfer 前端确定性测试与构建
	@cd transfer/frontend && npm test
	@cd transfer/frontend && npm run build

test-go: ## 使用临时 workspace 运行全部已跟踪 Go 模块测试
	@set -e; \
	workspace_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$workspace_dir"' EXIT; \
	workspace_file="$$workspace_dir/go.work"; \
	modules="$$(git ls-files -- 'go.mod' '**/go.mod' | sed 's#/go.mod$$##; s#^go.mod$$#.#')"; \
	if [ -z "$$modules" ]; then \
		echo "$(RED)仓库中没有已跟踪的 Go 模块$(NC)" >&2; \
		exit 1; \
	fi; \
	GOWORK="$$workspace_file" go work init $$(printf '%s\n' "$$modules" | sed "s#^#$(CURDIR)/#"); \
	for module in $$modules; do \
		echo "$(GREEN)运行 $$module 测试...$(NC)"; \
		(cd "$$module" && GOWORK="$$workspace_file" go test ./...); \
	done

compare-agent-eval: ## 比较两份仓库外 Agent v2 评测报告
	@bash scripts/test/agent-evaluation-gate.sh compare

compare-agent-eval-release: ## 按正式发布基线策略比较两份 Agent v2 报告
	@bash scripts/test/agent-evaluation-gate.sh compare-release

authorization-generate: ## 从 Manifest 生成 owner-local 常量和 System Tool Catalog
	@cd common && go run ./authorization/cmd/manifest --generate-owner-constants --repository-root ..
	@cd common && go run ./authorization/cmd/manifest --generate-tool-catalog --repository-root ..

test-authorization: ## 校验 IAM Manifest、生成常量和授权覆盖报告
	@cd common && go test ./authorization/... -count=1
	@cd common && go run ./authorization/cmd/manifest --check --repository-root .. > /tmp/addp-authorization-catalog.json
	@cd common && go run ./authorization/cmd/manifest --check-owner-constants --repository-root .. > /tmp/addp-owner-constants.json
	@cd common && go run ./authorization/cmd/manifest --check-tool-catalog --repository-root .. > /tmp/addp-system-tool-catalog.json
	@cd common && go run ./authorization/cmd/manifest --check-sql-seed --repository-root .. > /tmp/addp-iam-catalog-seed.json
	@cd common && go run ./authorization/cmd/manifest --coverage-report --repository-root .. > /tmp/addp-authorization-coverage.json
	@SWAGGER_COVERAGE_WARN_ONLY=1 bash scripts/swagger/check-route-coverage.sh all

test: test-execution-fixtures test-model-frontend test-agent-eval test-authorization test-go ## 运行所有测试
	@echo "$(GREEN)所有测试完成$(NC)"

test-system: ## 运行 System 模块测试
	@cd system/backend && go test ./...

db-migrate: ## 运行数据库迁移（重新初始化数据库）
	@echo "$(GREEN)运行数据库迁移...$(NC)"
	@docker compose -f docker-compose.infra.yml exec -T postgres psql -U addp -d addp < scripts/infra/init-db.sql
	@echo "$(GREEN)数据库迁移完成$(NC)"

db-shell: ## 连接到 PostgreSQL 数据库
	@docker compose -f docker-compose.infra.yml exec postgres psql -U addp -d addp

redis-cli: ## 连接到 Redis
	@docker compose -f docker-compose.infra.yml exec redis redis-cli -a addp_redis

minio-setup: ## 初始化 MinIO bucket (legacy, 使用 init-minio 替代)
	@echo "$(GREEN)初始化 MinIO...$(NC)"
	@docker compose -f docker-compose.infra.yml exec minio mc alias set local http://localhost:9000 minioadmin minioadmin
	@docker compose -f docker-compose.infra.yml exec minio mc mb local/addp-data --ignore-existing
	@echo "$(GREEN)MinIO 初始化完成$(NC)"

init-minio: ## 初始化 MinIO buckets（包括 PMTiles 快显缓存等）
	@./scripts/infra/init-minio.sh

init-minio-mvt: init-minio ## 初始化 MVT 瓦片缓存 bucket (alias for init-minio)

init-redis: ## 检查 Redis 缓存、事件和分布式锁
	@./scripts/infra/init-redis.sh

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
	@curl -s http://localhost:8180/health || echo "$(RED)  ✗ 不可用$(NC)"
	@echo ""
	@echo "PostgreSQL:"
	@docker compose -f docker-compose.infra.yml exec postgres pg_isready -U addp > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"
	@echo "Redis:"
	@docker compose -f docker-compose.infra.yml exec redis redis-cli -a addp_redis ping > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"
	@echo "MinIO:"
	@curl -s http://localhost:9000/minio/health/live > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"

backup: ## 备份数据库
	@echo "$(GREEN)备份数据库...$(NC)"
	@mkdir -p backups
	@docker compose -f docker-compose.infra.yml exec -T postgres pg_dump -U addp addp > backups/addp_$(shell date +%Y%m%d_%H%M%S).sql
	@echo "$(GREEN)数据库备份完成$(NC)"

restore: ## 恢复数据库（需要指定备份文件 FILE=xxx.sql）
	@if [ -z "$(FILE)" ]; then \
		echo "$(RED)错误: 请指定备份文件 FILE=xxx.sql$(NC)"; \
		exit 1; \
	fi
	@echo "$(YELLOW)恢复数据库: $(FILE)$(NC)"
	@docker compose -f docker-compose.infra.yml exec -T postgres psql -U addp -d addp < $(FILE)
	@echo "$(GREEN)数据库恢复完成$(NC)"

.PHONY: docs
docs: ## 生成 API 文档
	@echo "$(GREEN)生成 API 文档...$(NC)"
	@echo "$(YELLOW)TODO: 实现 API 文档生成$(NC)"

check-frontend: ## 检查所有 frontend 的 Docker 配置是否符合规范
	@echo "$(GREEN)检查 frontend Docker 配置...$(NC)"
	@./scripts/utils/standardize-frontend-docker.sh

fix-frontend: ## 自动修复 frontend Docker 配置问题（创建缺失的 .dockerignore）
	@echo "$(GREEN)修复 frontend Docker 配置...$(NC)"
	@./scripts/utils/standardize-frontend-docker.sh --fix

registry-start: ## 启动本地 Docker Registry（镜像构建必需）
	@echo "$(GREEN)启动本地 Docker Registry...$(NC)"
	@./scripts/registry/start.sh

registry-stop: ## 停止本地 Docker Registry
	@echo "$(YELLOW)停止本地 Docker Registry...$(NC)"
	@docker stop registry || true
	@echo "$(GREEN)Registry 已停止$(NC)"

registry-restart: ## 重启本地 Docker Registry
	@echo "$(YELLOW)重启本地 Docker Registry...$(NC)"
	@docker rm -f registry 2>/dev/null || true
	@./scripts/setup/start-registry.sh

registry-status: ## 检查本地 Docker Registry 状态
	@./scripts/setup/check-registry.sh

# ==================== 生产环境命令 ====================

prod-up-infra: ## 启动基础设施层（Postgres, Redis, MinIO, Meilisearch）
	@echo "$(GREEN)启动基础设施层...$(NC)"
	@docker compose -f docker-compose.yml --profile infra up -d
	@echo "$(GREEN)等待基础设施就绪...$(NC)"
	@bash scripts/prod/wait-infra.sh
	@echo "$(GREEN)基础设施已就绪！$(NC)"

prod-down-infra: ## 停止基础设施层
	@echo "$(YELLOW)停止基础设施层...$(NC)"
	@docker compose -f docker-compose.yml --profile infra down
	@echo "$(GREEN)基础设施已停止$(NC)"

prod-restart-infra: ## 重启基础设施层
	@$(MAKE) prod-down-infra
	@$(MAKE) prod-up-infra

prod-up-addp: ## 启动所有 ADDP 应用服务（需要先启动 infra）
	@echo "$(GREEN)启动 ADDP 应用服务...$(NC)"
	@docker compose -f docker-compose.yml --profile addp up -d
	@echo "$(GREEN)ADDP 应用服务已启动$(NC)"
	@$(MAKE) prod-status

prod-down-addp: ## 停止所有 ADDP 应用服务（保留基础设施）
	@echo "$(YELLOW)停止 ADDP 应用服务...$(NC)"
	@docker compose -f docker-compose.yml --profile addp down
	@echo "$(GREEN)ADDP 应用服务已停止（基础设施保持运行）$(NC)"

prod-restart-addp: ## 重启所有 ADDP 应用服务
	@$(MAKE) prod-down-addp
	@$(MAKE) prod-up-addp

prod-up: ## 启动完整平台（基础设施 + ADDP 应用）
	@echo "$(GREEN)启动完整 ADDP 平台...$(NC)"
	@docker compose -f docker-compose.yml --profile infra --profile addp up -d
	@echo "$(GREEN)完整平台已启动$(NC)"
	@$(MAKE) prod-status

prod-down: ## 停止完整平台
	@echo "$(YELLOW)停止完整平台...$(NC)"
	@docker compose -f docker-compose.yml --profile infra --profile addp down
	@echo "$(GREEN)完整平台已停止$(NC)"

prod-restart: ## 重启完整平台
	@$(MAKE) prod-down
	@$(MAKE) prod-up

prod-logs-infra: ## 查看基础设施日志
	@docker compose -f docker-compose.yml logs -f postgres redis minio meilisearch

prod-logs-addp: ## 查看所有 ADDP 应用日志
	@docker compose -f docker-compose.yml --profile addp logs -f

prod-logs-orchestrator: ## 查看 Orchestrator 日志
	@docker compose -f docker-compose.yml logs -f orchestrator-backend orchestrator-frontend

prod-logs-develop: ## 查看 Develop 日志
	@docker compose -f docker-compose.yml logs -f develop-backend develop-frontend

prod-status: ## 显示所有服务状态和访问地址
	@echo "$(GREEN)生产环境服务状态:$(NC)"
	@docker compose -f docker-compose.yml --profile infra --profile addp ps
	@echo ""
	@echo "$(YELLOW)访问地址:$(NC)"
	@echo "  Console (控制台):      http://localhost:8000"
	@echo "  System 管理界面:        http://localhost:8090"
	@echo "  Manager 管理界面:       http://localhost:8091"
	@echo "  Meta 管理界面:          http://localhost:8092"
	@echo "  Transfer 管理界面:      http://localhost:8093"
	@echo "  Orchestrator 管理界面:  http://localhost:8094"
	@echo "  Develop SQL 工作台:     http://localhost:8095"
	@echo ""
	@echo "$(YELLOW)API 端点:$(NC)"
	@echo "  Gateway API:            http://localhost:8000/api"
	@echo "  System Backend:         http://localhost:8180"
	@echo "  Manager Backend:        http://localhost:8081"
	@echo "  Meta Backend:           http://localhost:8082"
	@echo "  Transfer Backend:       http://localhost:8083"
	@echo "  Orchestrator Backend:   http://localhost:8084"
	@echo "  Develop Backend:        http://localhost:8185"

prod-health: ## 检查所有服务健康状态
	@bash scripts/prod/health-check.sh

# 向后兼容别名（可选）
prod-start: prod-up  ## 别名：启动生产环境
prod-stop: prod-down ## 别名：停止生产环境
prod-logs: prod-logs-addp ## 别名：查看应用日志
