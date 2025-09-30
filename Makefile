.PHONY: help init dev build up down logs clean test

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

init: ## 初始化项目（创建必要的目录和配置文件）
	@echo "$(GREEN)初始化项目...$(NC)"
	@mkdir -p system/data
	@mkdir -p scripts
	@if [ ! -f .env ]; then cp .env.example .env && echo "$(GREEN)已创建 .env 文件$(NC)"; fi
	@echo "$(GREEN)初始化完成！$(NC)"

dev-system: ## 开发模式运行 System 模块
	@echo "$(GREEN)启动 System 模块开发环境...$(NC)"
	@cd system && $(MAKE) dev

dev-manager: ## 开发模式运行 Manager 模块
	@echo "$(GREEN)启动 Manager 模块开发环境...$(NC)"
	@cd manager/backend && go run cmd/server/main.go

dev-meta: ## 开发模式运行 Meta 模块
	@echo "$(GREEN)启动 Meta 模块开发环境...$(NC)"
	@cd meta/backend && go run cmd/server/main.go

dev-transfer: ## 开发模式运行 Transfer 模块
	@echo "$(GREEN)启动 Transfer 模块开发环境...$(NC)"
	@cd transfer/backend && go run cmd/server/main.go

dev-gateway: ## 开发模式运行 Gateway 模块
	@echo "$(GREEN)启动 Gateway 模块开发环境...$(NC)"
	@cd gateway && go run cmd/gateway/main.go

build: ## 编译所有服务
	@echo "$(GREEN)编译所有服务...$(NC)"
	@cd system/backend && go build -o ../../bin/system cmd/server/main.go
	@echo "$(GREEN)System 编译完成$(NC)"
	@if [ -d gateway/cmd ]; then cd gateway && go build -o ../bin/gateway cmd/gateway/main.go && echo "$(GREEN)Gateway 编译完成$(NC)"; fi
	@if [ -d manager/backend/cmd ]; then cd manager/backend && go build -o ../../bin/manager cmd/server/main.go && echo "$(GREEN)Manager 编译完成$(NC)"; fi
	@if [ -d meta/backend/cmd ]; then cd meta/backend && go build -o ../../bin/meta cmd/server/main.go && echo "$(GREEN)Meta 编译完成$(NC)"; fi
	@if [ -d transfer/backend/cmd ]; then cd transfer/backend && go build -o ../../bin/transfer cmd/server/main.go && echo "$(GREEN)Transfer 编译完成$(NC)"; fi
	@echo "$(GREEN)所有服务编译完成！$(NC)"

docker-build: ## 构建 Docker 镜像（仅 System 模块）
	@echo "$(GREEN)构建 System 模块 Docker 镜像...$(NC)"
	@docker-compose build system-backend system-frontend
	@echo "$(GREEN)构建完成！$(NC)"

docker-build-all: ## 构建所有服务的 Docker 镜像
	@echo "$(GREEN)构建所有服务的 Docker 镜像...$(NC)"
	@docker-compose --profile full build
	@echo "$(GREEN)所有镜像构建完成！$(NC)"

up: ## 启动 System 模块（基础服务）
	@echo "$(GREEN)启动 System 模块...$(NC)"
	@docker-compose up -d system-backend system-frontend
	@echo "$(GREEN)System 模块已启动！$(NC)"
	@echo "$(YELLOW)访问地址:$(NC)"
	@echo "  - System Backend:  http://localhost:8080"
	@echo "  - System Frontend: http://localhost:8090"

up-full: ## 启动所有服务（完整平台）
	@echo "$(GREEN)启动完整平台（所有服务）...$(NC)"
	@docker-compose --profile full up -d
	@echo "$(GREEN)所有服务已启动！$(NC)"
	@$(MAKE) status

up-infra: ## 仅启动基础设施服务（PostgreSQL, Redis, MinIO）
	@echo "$(GREEN)启动基础设施服务...$(NC)"
	@docker-compose up -d postgres redis minio
	@echo "$(GREEN)基础设施服务已启动！$(NC)"

down: ## 停止所有服务
	@echo "$(YELLOW)停止所有服务...$(NC)"
	@docker-compose --profile full down
	@echo "$(GREEN)所有服务已停止$(NC)"

restart: down up ## 重启 System 模块

restart-full: down up-full ## 重启所有服务

logs: ## 查看所有服务日志
	@docker-compose --profile full logs -f

logs-system: ## 查看 System 模块日志
	@docker-compose logs -f system-backend system-frontend

logs-manager: ## 查看 Manager 模块日志
	@docker-compose logs -f manager-backend

logs-meta: ## 查看 Meta 模块日志
	@docker-compose logs -f meta-backend

logs-transfer: ## 查看 Transfer 模块日志
	@docker-compose logs -f transfer-backend transfer-worker

logs-gateway: ## 查看 Gateway 模块日志
	@docker-compose logs -f gateway

status: ## 显示所有服务状态
	@echo "$(GREEN)服务状态:$(NC)"
	@docker-compose --profile full ps
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
	@echo "  - MinIO Console:    http://localhost:9001"
	@echo "  - MinIO API:        http://localhost:9000"

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
		docker-compose --profile full down -v; \
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
	@docker-compose exec -T postgres psql -U addp -d addp < scripts/init-db.sql
	@echo "$(GREEN)数据库迁移完成$(NC)"

db-shell: ## 连接到 PostgreSQL 数据库
	@docker-compose exec postgres psql -U addp -d addp

redis-cli: ## 连接到 Redis
	@docker-compose exec redis redis-cli -a addp_redis

minio-setup: ## 初始化 MinIO bucket
	@echo "$(GREEN)初始化 MinIO...$(NC)"
	@docker-compose exec minio mc alias set local http://localhost:9000 minioadmin minioadmin
	@docker-compose exec minio mc mb local/addp-data --ignore-existing
	@echo "$(GREEN)MinIO 初始化完成$(NC)"

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
	@docker-compose exec postgres pg_isready -U addp > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"
	@echo "Redis:"
	@docker-compose exec redis redis-cli -a addp_redis ping > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"
	@echo "MinIO:"
	@curl -s http://localhost:9000/minio/health/live > /dev/null 2>&1 && echo "  $(GREEN)✓ 正常$(NC)" || echo "  $(RED)✗ 不可用$(NC)"

backup: ## 备份数据库
	@echo "$(GREEN)备份数据库...$(NC)"
	@mkdir -p backups
	@docker-compose exec -T postgres pg_dump -U addp addp > backups/addp_$(shell date +%Y%m%d_%H%M%S).sql
	@echo "$(GREEN)数据库备份完成$(NC)"

restore: ## 恢复数据库（需要指定备份文件 FILE=xxx.sql）
	@if [ -z "$(FILE)" ]; then \
		echo "$(RED)错误: 请指定备份文件 FILE=xxx.sql$(NC)"; \
		exit 1; \
	fi
	@echo "$(YELLOW)恢复数据库: $(FILE)$(NC)"
	@docker-compose exec -T postgres psql -U addp -d addp < $(FILE)
	@echo "$(GREEN)数据库恢复完成$(NC)"

.PHONY: docs
docs: ## 生成 API 文档
	@echo "$(GREEN)生成 API 文档...$(NC)"
	@echo "$(YELLOW)TODO: 实现 API 文档生成$(NC)"