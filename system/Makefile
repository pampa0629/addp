.PHONY: help dev build docker-build docker-up docker-down clean

help:
	@echo "可用命令:"
	@echo "  make dev          - 开发模式运行（后端）"
	@echo "  make build        - 编译后端"
	@echo "  make docker-build - 构建 Docker 镜像"
	@echo "  make docker-up    - 启动 Docker 容器"
	@echo "  make docker-down  - 停止 Docker 容器"
	@echo "  make clean        - 清理编译文件"

dev:
	cd backend && go run cmd/server/main.go

build:
	cd backend && go build -o ../bin/server cmd/server/main.go

docker-build:
	docker-compose build

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

clean:
	rm -rf bin/
	rm -rf data/*.db