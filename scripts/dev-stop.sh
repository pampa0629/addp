#!/bin/bash

echo "🛑 停止 ADDP 开发环境"

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 停止 Go 进程（使用 PID 文件）
if [ -d ".dev-pids" ]; then
  echo -e "${YELLOW}停止后端服务...${NC}"

  if [ -f ".dev-pids/system.pid" ]; then
    SYSTEM_PID=$(cat .dev-pids/system.pid)
    if ps -p $SYSTEM_PID > /dev/null 2>&1; then
      kill $SYSTEM_PID
      echo "✓ System Backend 已停止 (PID: $SYSTEM_PID)"
    fi
  fi

  if [ -f ".dev-pids/manager.pid" ]; then
    MANAGER_PID=$(cat .dev-pids/manager.pid)
    if ps -p $MANAGER_PID > /dev/null 2>&1; then
      kill $MANAGER_PID
      echo "✓ Manager Backend 已停止 (PID: $MANAGER_PID)"
    fi
  fi

  if [ -f ".dev-pids/meta.pid" ]; then
    META_PID=$(cat .dev-pids/meta.pid)
    if ps -p $META_PID > /dev/null 2>&1; then
      kill $META_PID
      echo "✓ Meta Backend 已停止 (PID: $META_PID)"
    fi
  fi

  if [ -f ".dev-pids/gateway.pid" ]; then
    GATEWAY_PID=$(cat .dev-pids/gateway.pid)
    if ps -p $GATEWAY_PID > /dev/null 2>&1; then
      kill $GATEWAY_PID
      echo "✓ Gateway 已停止 (PID: $GATEWAY_PID)"
    fi
  fi

  # 清理 PID 文件
  rm -rf .dev-pids
else
  # 备用方案：通过进程名停止
  echo -e "${YELLOW}使用进程名停止后端服务...${NC}"
  pkill -f "go run cmd/server/main.go"
  pkill -f "go run cmd/gateway/main.go"
  echo "✓ 后端服务已停止"
fi

# 停止 npm 进程
echo -e "${YELLOW}停止前端服务...${NC}"
pkill -f "vite"
echo "✓ 前端服务已停止"

# 停止 Docker 基础设施
echo -e "${YELLOW}停止基础设施...${NC}"
docker-compose stop postgres redis minio
echo "✓ 基础设施已停止"

echo ""
echo -e "${GREEN}✓ 所有服务已停止${NC}"
