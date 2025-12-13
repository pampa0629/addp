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
      # 等待进程真正退出（最多等待10秒）
      for i in {1..10}; do
        if ! ps -p $SYSTEM_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ System Backend 已停止 (PID: $SYSTEM_PID)"
    fi
  fi

  if [ -f ".dev-pids/manager.pid" ]; then
    MANAGER_PID=$(cat .dev-pids/manager.pid)
    if ps -p $MANAGER_PID > /dev/null 2>&1; then
      kill $MANAGER_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $MANAGER_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Manager Backend 已停止 (PID: $MANAGER_PID)"
    fi
  fi

  if [ -f ".dev-pids/meta.pid" ]; then
    META_PID=$(cat .dev-pids/meta.pid)
    if ps -p $META_PID > /dev/null 2>&1; then
      kill $META_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $META_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Meta Backend 已停止 (PID: $META_PID)"
    fi
  fi

  if [ -f ".dev-pids/transfer.pid" ]; then
    TRANSFER_PID=$(cat .dev-pids/transfer.pid)
    if ps -p $TRANSFER_PID > /dev/null 2>&1; then
      kill $TRANSFER_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $TRANSFER_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Transfer Backend 已停止 (PID: $TRANSFER_PID)"
    fi
  fi

  if [ -f ".dev-pids/transfer-worker.pid" ]; then
    TRANSFER_WORKER_PID=$(cat .dev-pids/transfer-worker.pid)
    if ps -p $TRANSFER_WORKER_PID > /dev/null 2>&1; then
      kill $TRANSFER_WORKER_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $TRANSFER_WORKER_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Transfer Worker 已停止 (PID: $TRANSFER_WORKER_PID)"
    fi
  fi

  if [ -f ".dev-pids/meta-worker.pid" ]; then
    META_WORKER_PID=$(cat .dev-pids/meta-worker.pid)
    if ps -p $META_WORKER_PID > /dev/null 2>&1; then
      kill $META_WORKER_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $META_WORKER_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Meta Worker 已停止 (PID: $META_WORKER_PID)"
    fi
  fi

  if [ -f ".dev-pids/manager-worker.pid" ]; then
    MANAGER_WORKER_PID=$(cat .dev-pids/manager-worker.pid)
    if ps -p $MANAGER_WORKER_PID > /dev/null 2>&1; then
      kill $MANAGER_WORKER_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $MANAGER_WORKER_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Manager Worker 已停止 (PID: $MANAGER_WORKER_PID)"
    fi
  fi

  if [ -f ".dev-pids/orchestrator-backend.pid" ]; then
    ORCHESTRATOR_PID=$(cat .dev-pids/orchestrator-backend.pid)
    if ps -p $ORCHESTRATOR_PID > /dev/null 2>&1; then
      kill $ORCHESTRATOR_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $ORCHESTRATOR_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Orchestrator Backend 已停止 (PID: $ORCHESTRATOR_PID)"
    fi
    rm -f ".dev-pids/orchestrator-backend.pid"
  fi

  if [ -f ".dev-pids/develop.pid" ]; then
    DEVELOP_PID=$(cat .dev-pids/develop.pid)
    if ps -p $DEVELOP_PID > /dev/null 2>&1; then
      kill $DEVELOP_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $DEVELOP_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Develop Backend 已停止 (PID: $DEVELOP_PID)"
    fi
    rm -f ".dev-pids/develop.pid"
  fi

  # Stop GeoPandas Engine (Python service)
  if [ -f ".dev-pids/geopandas-engine.pid" ]; then
    GEOPANDAS_PID=$(cat .dev-pids/geopandas-engine.pid)
    if ps -p $GEOPANDAS_PID > /dev/null 2>&1; then
      echo "停止 GeoPandas Engine (PID: $GEOPANDAS_PID)..."
      kill $GEOPANDAS_PID
      # 等待进程真正退出（最多等待10秒）
      for i in {1..10}; do
        if ! ps -p $GEOPANDAS_PID > /dev/null 2>&1; then
          echo "✓ GeoPandas Engine 已停止"
          break
        fi
        sleep 0.5
      done
      # 如果还在运行，强制杀掉
      if ps -p $GEOPANDAS_PID > /dev/null 2>&1; then
        echo "强制停止 GeoPandas Engine..."
        kill -9 $GEOPANDAS_PID 2>/dev/null || true
      fi
    fi
    rm -f ".dev-pids/geopandas-engine.pid"
  fi

  # 清理可能的 Python 进程残留
  pkill -f "python.*api_server.py" 2>/dev/null || true

  if [ -f ".dev-pids/gateway.pid" ]; then
    GATEWAY_PID=$(cat .dev-pids/gateway.pid)
    if ps -p $GATEWAY_PID > /dev/null 2>&1; then
      kill $GATEWAY_PID
      # 等待进程真正退出
      for i in {1..10}; do
        if ! ps -p $GATEWAY_PID > /dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
      echo "✓ Gateway 已停止 (PID: $GATEWAY_PID)"
    fi
  fi

fi

# 停止前端服务
echo -e "${YELLOW}停止前端服务...${NC}"

if [ -d ".dev-pids" ]; then
  if [ -f ".dev-pids/portal-frontend.pid" ]; then
    PORTAL_FE_PID=$(cat .dev-pids/portal-frontend.pid)
    if ps -p $PORTAL_FE_PID > /dev/null 2>&1; then
      kill $PORTAL_FE_PID 2>/dev/null || true
      echo "✓ Portal Frontend 已停止 (PID: $PORTAL_FE_PID)"
    fi
  fi

  if [ -f ".dev-pids/system-frontend.pid" ]; then
    SYSTEM_FE_PID=$(cat .dev-pids/system-frontend.pid)
    if ps -p $SYSTEM_FE_PID > /dev/null 2>&1; then
      kill $SYSTEM_FE_PID 2>/dev/null || true
      echo "✓ System Frontend 已停止 (PID: $SYSTEM_FE_PID)"
    fi
  fi

  if [ -f ".dev-pids/manager-frontend.pid" ]; then
    MANAGER_FE_PID=$(cat .dev-pids/manager-frontend.pid)
    if ps -p $MANAGER_FE_PID > /dev/null 2>&1; then
      kill $MANAGER_FE_PID 2>/dev/null || true
      echo "✓ Manager Frontend 已停止 (PID: $MANAGER_FE_PID)"
    fi
  fi

  if [ -f ".dev-pids/meta-frontend.pid" ]; then
    META_FE_PID=$(cat .dev-pids/meta-frontend.pid)
    if ps -p $META_FE_PID > /dev/null 2>&1; then
      kill $META_FE_PID 2>/dev/null || true
      echo "✓ Meta Frontend 已停止 (PID: $META_FE_PID)"
    fi
  fi

  if [ -f ".dev-pids/transfer-frontend.pid" ]; then
    TRANSFER_FE_PID=$(cat .dev-pids/transfer-frontend.pid)
    if ps -p $TRANSFER_FE_PID > /dev/null 2>&1; then
      kill $TRANSFER_FE_PID 2>/dev/null || true
      echo "✓ Transfer Frontend 已停止 (PID: $TRANSFER_FE_PID)"
    fi
  fi

  if [ -f ".dev-pids/orchestrator-frontend.pid" ]; then
    ORCHESTRATOR_FE_PID=$(cat .dev-pids/orchestrator-frontend.pid)
    if ps -p $ORCHESTRATOR_FE_PID > /dev/null 2>&1; then
      kill $ORCHESTRATOR_FE_PID 2>/dev/null || true
      echo "✓ Orchestrator Frontend 已停止 (PID: $ORCHESTRATOR_FE_PID)"
    fi
    rm -f ".dev-pids/orchestrator-frontend.pid"
  fi

  if [ -f ".dev-pids/develop-frontend.pid" ]; then
    DEVELOP_FE_PID=$(cat .dev-pids/develop-frontend.pid)
    if ps -p $DEVELOP_FE_PID > /dev/null 2>&1; then
      kill $DEVELOP_FE_PID 2>/dev/null || true
      echo "✓ Develop Frontend 已停止 (PID: $DEVELOP_FE_PID)"
    fi
    rm -f ".dev-pids/develop-frontend.pid"
  fi
fi

# 清理任何残留的 vite 进程
pkill -f "vite" 2>/dev/null || true

# 等待 vite 进程退出
for i in {1..10}; do
  if ! pgrep -f "vite" > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

# 清理 Vite 缓存（避免旧代码被缓存）
echo -e "${YELLOW}清理前端缓存...${NC}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

for frontend_dir in "$ROOT_DIR/portal/frontend" "$ROOT_DIR/system/frontend" "$ROOT_DIR/manager/frontend" "$ROOT_DIR/meta/frontend" "$ROOT_DIR/transfer/frontend" "$ROOT_DIR/orchestrator/frontend" "$ROOT_DIR/develop/frontend"; do
  if [ -d "$frontend_dir" ]; then
    rm -rf "$frontend_dir/node_modules/.vite" "$frontend_dir/.vite" 2>/dev/null || true
  fi
done
echo "✓ 前端缓存已清理"

# 清理 PID 文件
if [ -d ".dev-pids" ]; then
  rm -rf .dev-pids
fi

# 无论是否使用了 PID 文件，都要确保清理所有残留进程
echo -e "${YELLOW}检查并清理残留进程...${NC}"

# 定义要清理的模块（格式：目录路径|命令模式）
MODULES=(
  "system/backend|go run cmd/server/main.go"
  "manager/backend|go run cmd/server/main.go"
  "meta/backend|go run cmd/server/main.go"
  "transfer/backend|go run cmd/server/main.go"
  "transfer/backend|go run cmd/worker/main.go"
  "meta/backend|go run cmd/worker/main.go"
  "manager/backend|go run cmd/worker/main.go"
  "orchestrator/backend|go run cmd/server/main.go"
  "develop/backend|go run cmd/server/main.go"
  "gateway|go run cmd/gateway/main.go"
)

for entry in "${MODULES[@]}"; do
  module_path="${entry%%|*}"
  cmd_pattern="${entry##*|}"

  # 查找所有匹配该命令的进程
  for pid in $(pgrep -f "$cmd_pattern"); do
    if ps -p $pid > /dev/null 2>&1; then
      # 检查进程的工作目录是否匹配该模块
      cwd=$(lsof -p $pid 2>/dev/null | grep cwd | awk '{print $NF}')
      if [[ "$cwd" == *"$module_path"* ]]; then
        echo "发现残留的进程: ${module_path} (PID: $pid)"
        # 杀死进程树（父进程和所有子进程）
        pkill -TERM -P $pid 2>/dev/null  # 先杀子进程
        kill -TERM $pid 2>/dev/null       # 再杀父进程
      fi
    fi
  done
done

# 等待进程退出（最多5秒）
for i in {1..10}; do
  if ! pgrep -f "go run cmd/server/main.go\|go run cmd/gateway/main.go" > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

# 强制杀死任何残留的编译后的 main 进程
pkill -f "/go-build.*/main$" 2>/dev/null || true

echo "✓ 后端服务已停止"

# 停止 Docker 基础设施
# echo -e "${YELLOW}停止基础设施...${NC}"
# docker-compose stop postgres redis minio
# echo "✓ 基础设施已停止"

echo ""
echo -e "${GREEN}✓ 所有服务已停止${NC}"
