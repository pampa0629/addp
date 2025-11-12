.PHONY: help init up down logs dev-backend dev-frontend dev clean test

# 默认目标
help:
	@echo "MVT 海量空间数据快显系统 - Make 命令"
	@echo ""
	@echo "使用方法:"
	@echo "  make init          - 初始化项目（安装依赖、创建配置文件）"
	@echo "  make up            - 启动 Docker 基础设施（PostGIS + Redis）"
	@echo "  make down          - 停止 Docker 服务"
	@echo "  make logs          - 查看 Docker 日志"
	@echo "  make dev-backend   - 运行后端开发服务器"
	@echo "  make dev-frontend  - 运行前端开发服务器"
	@echo "  make dev           - 同时运行后端和前端（推荐）"
	@echo "  make test          - 运行测试"
	@echo "  make clean         - 清理临时文件"

# 初始化项目
init:
	@echo "==> 初始化 MVT 项目..."
	@echo "==> 创建后端配置文件..."
	@if [ ! -f backend/config/datasources.yaml ]; then \
		cp backend/config/datasources.yaml.example backend/config/datasources.yaml; \
		echo "    ✓ 已创建 backend/config/datasources.yaml"; \
	else \
		echo "    ✓ backend/config/datasources.yaml 已存在"; \
	fi
	@if [ ! -f backend/config/app.yaml ]; then \
		cp backend/config/app.yaml.example backend/config/app.yaml; \
		echo "    ✓ 已创建 backend/config/app.yaml"; \
	else \
		echo "    ✓ backend/config/app.yaml 已存在"; \
	fi
	@echo "==> 安装后端依赖..."
	cd backend && go mod download
	@echo "==> 安装前端依赖..."
	cd frontend && npm install
	@echo "==> ✓ 初始化完成！"
	@echo ""
	@echo "下一步:"
	@echo "  1. 启动基础设施: make up"
	@echo "  2. 准备测试数据（参考 QUICKSTART.md）"
	@echo "  3. 运行开发服务器: make dev"

# 启动 Docker 基础设施
up:
	@echo "==> 启动 PostGIS 和 Redis..."
	docker-compose up -d
	@echo "==> 等待服务就绪..."
	@sleep 5
	@echo "==> ✓ 服务已启动"
	@echo ""
	@echo "服务状态:"
	@docker-compose ps

# 停止 Docker 服务
down:
	@echo "==> 停止服务..."
	docker-compose down
	@echo "==> ✓ 服务已停止"

# 查看日志
logs:
	docker-compose logs -f

# 运行后端开发服务器
dev-backend:
	@echo "==> 启动后端服务器..."
	cd backend && go run cmd/server/main.go

# 运行前端开发服务器
dev-frontend:
	@echo "==> 启动前端开发服务器..."
	cd frontend && npm run dev

# 同时运行后端和前端
dev:
	@echo "==> 启动开发环境..."
	@echo "==> 后端端口: 8090"
	@echo "==> 前端端口: 5180"
	@echo ""
	@$(MAKE) -j2 dev-backend dev-frontend

# 运行测试
test:
	@echo "==> 运行后端测试..."
	cd backend && go test -v ./...

# 清理
clean:
	@echo "==> 清理临时文件..."
	@rm -rf backend/logs/*
	@echo "==> ✓ 清理完成"

# 数据库相关命令
.PHONY: db-shell db-init


# 初始化测试数据
db-init:
	@echo "==> 初始化测试数据..."
	@echo "请按照 QUICKSTART.md 中的步骤手动导入数据"

# Redis 相关命令
.PHONY: redis-cli redis-flush

# 连接到 Redis
redis-cli:
	@echo "==> 连接到 Redis..."
	docker exec -it mvt-redis redis-cli

# 清空 Redis 缓存
redis-flush:
	@echo "==> 清空 Redis 缓存..."
	docker exec -it mvt-redis redis-cli FLUSHDB
	@echo "==> ✓ 缓存已清空"

# 构建相关
.PHONY: build build-backend build-frontend

# 构建所有
build: build-backend build-frontend

# 构建后端
build-backend:
	@echo "==> 构建后端..."
	cd backend && go build -o ../dist/mvt-server cmd/server/main.go
	@echo "==> ✓ 后端构建完成: dist/mvt-server"

# 构建前端
build-frontend:
	@echo "==> 构建前端..."
	cd frontend && npm run build
	@echo "==> ✓ 前端构建完成: frontend/dist/"
