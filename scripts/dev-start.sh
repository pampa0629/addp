#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${ROOT_DIR}"

export PROJECT_ROOT="${ROOT_DIR}"
mkdir -p logs

# 本地 Go 构建缓存，避免写入系统 GOPATH，并优先使用本机 Go 工具链
export GOMODCACHE="${PROJECT_ROOT}/.gomodcache"
export GOPATH="${PROJECT_ROOT}/.gopath"
export GOTOOLCHAIN="local"

# Honor GOPROXY from environment/.env to avoid module i/o timeouts
if [ -n "${GOPROXY}" ]; then
  export GOPROXY
  echo "Go module proxy set: ${GOPROXY}"
fi

if [ -f "${ROOT_DIR}/.env" ]; then
  set -a
  # shellcheck source=/dev/null
  source "${ROOT_DIR}/.env"
  set +a
fi

export ELASTICSEARCH_URL="${ELASTICSEARCH_URL_LOCAL:-http://localhost:9200}"

echo "🚀 启动 ADDP 开发环境"
echo ""

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

PORTAL_FE_PORT=${PORTAL_FE_PORT:-5170}
SYSTEM_FE_PORT=${SYSTEM_FE_PORT:-5173}
MANAGER_FE_PORT=${MANAGER_FE_PORT:-5174}
META_FE_PORT=${META_FE_PORT:-5175}
TRANSFER_FE_PORT=${TRANSFER_FE_PORT:-5176}

# 1. 启动基础设施
# echo -e "${YELLOW}Step 1/5: 启动基础设施（PostgreSQL, Redis, MinIO）${NC}"
# docker-compose up -d postgres redis minio
# sleep 5

# # 等待基础设施就绪
# echo "等待 PostgreSQL 就绪..."
# until docker exec addp-postgres pg_isready -U addp > /dev/null 2>&1; do
#   echo -n "."
#   sleep 1
# done
# echo -e "${GREEN}✓ PostgreSQL 就绪${NC}"

# echo "等待 Redis 就绪..."
# until docker exec addp-redis redis-cli ping > /dev/null 2>&1; do
#   echo -n "."
#   sleep 1
# done
# echo -e "${GREEN}✓ Redis 就绪${NC}"

# echo "等待 MinIO 就绪..."
# until curl -f http://localhost:9002/minio/health/live > /dev/null 2>&1; do
#   echo -n "."
#   sleep 1
# done
# echo -e "${GREEN}✓ MinIO 就绪${NC}"
# echo ""

# 2. 启动 System Backend
echo -e "${YELLOW}Step 2/5: 启动 System Backend${NC}"
cd system/backend
go run cmd/server/main.go 2> ../../logs/system-backend-stderr.log &
SYSTEM_PID=$!
cd ../..

# 等待 System Backend 就绪
echo "等待 System Backend 就绪..."
MAX_WAIT=60
WAIT_COUNT=0
until curl -f http://localhost:8080/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ System Backend 启动超时${NC}"
    echo "查看日志: tail -f logs/system-backend.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ System Backend 就绪 (PID: $SYSTEM_PID)${NC}"
echo ""

# 3. 启动 Manager Backend
echo -e "${YELLOW}Step 3/5: 启动 Manager Backend${NC}"
cd manager/backend
go run cmd/server/main.go 2> ../../logs/manager-backend-stderr.log &
MANAGER_PID=$!
cd ../..

# 等待 Manager Backend 就绪
echo "等待 Manager Backend 就绪..."
WAIT_COUNT=0
until curl -f http://localhost:8081/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Manager Backend 启动超时${NC}"
    echo "查看日志: tail -f logs/manager-backend.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ Manager Backend 就绪 (PID: $MANAGER_PID)${NC}"
echo ""

# 4. 启动 Meta Backend
echo -e "${YELLOW}Step 4/6: 启动 Meta Backend${NC}"
cd meta/backend
go run cmd/server/main.go 2> ../../logs/meta-backend-stderr.log &
META_PID=$!
cd ../..

# 等待 Meta Backend 就绪
echo "等待 Meta Backend 就绪..."
WAIT_COUNT=0
until curl -f http://localhost:8082/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Meta Backend 启动超时${NC}"
    echo "查看日志: tail -f logs/meta-backend.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ Meta Backend 就绪 (PID: $META_PID)${NC}"
echo ""

# 5. 启动 Transfer Backend
echo -e "${YELLOW}Step 5/7: 启动 Transfer Backend${NC}"
cd transfer/backend
go mod download >/dev/null 2>> ../../logs/transfer-backend-stderr.log || true
go run cmd/server/main.go 2> ../../logs/transfer-backend-stderr.log &
TRANSFER_PID=$!
cd ../..

# 等待 Transfer Backend 就绪
echo "等待 Transfer Backend 就绪..."
WAIT_COUNT=0
until curl -f http://localhost:8083/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Transfer Backend 启动超时${NC}"
    echo "查看日志: tail -f logs/transfer-backend-stderr.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ Transfer Backend 就绪 (PID: $TRANSFER_PID)${NC}"
echo ""

# 6. 启动 Transfer Worker
echo -e "${YELLOW}Step 6/7: 启动 Transfer Worker${NC}"
cd transfer/backend
go mod download >/dev/null 2>> ../../logs/transfer-worker.log || true
go run cmd/worker/main.go > ../../logs/transfer-worker.log 2>&1 &
TRANSFER_WORKER_PID=$!
cd ../..
echo -e "${GREEN}✓ Transfer Worker 启动中 (PID: $TRANSFER_WORKER_PID)${NC}"
echo "  注意: Transfer Worker 依赖 Redis，请确保 Redis 已启动"
echo ""

# 7. 启动 Gateway
echo -e "${YELLOW}Step 7/7: 启动 Gateway${NC}"
cd gateway
go run cmd/gateway/main.go 2> ../logs/gateway-stderr.log &
GATEWAY_PID=$!
cd ..

# 等待 Gateway 就绪
echo "等待 Gateway 就绪..."
WAIT_COUNT=0
until curl -f http://localhost:8000/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Gateway 启动超时${NC}"
    echo "查看日志: tail -f logs/gateway.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ Gateway 就绪 (PID: $GATEWAY_PID)${NC}"
echo ""

# 6. 启动前端服务
echo -e "${YELLOW}Step 6/6: 启动前端服务${NC}"

# 创建 PID 目录
mkdir -p .dev-pids

# 等待 HTTP 服务就绪的小工具函数
wait_for_http() {
  local name="$1"
  local url="$2"
  local max_wait="${3:-60}"
  local count=0
  echo "等待 ${name} 就绪 (${url})..."
  until curl -fsS "$url" > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    count=$((count + 1))
    if [ "$count" -ge "$max_wait" ]; then
      echo -e "\n${RED}✗ ${name} 启动超时${NC}"
      return 1
    fi
  done
  echo -e "\n${GREEN}✓ ${name} 就绪${NC}"
}

# 缺依赖时自动安装
ensure_node_modules() {
  local dir="$1"
  local vite_bin="$dir/node_modules/.bin/vite"
  if [ ! -d "$dir/node_modules" ]; then
    echo "检测到 $dir 缺少依赖，执行 npm install ..."
    (cd "$dir" && npm install)
  fi
  # 某些情况下 node_modules 存在但 devDependencies 未装全，补装一次
  if [ ! -x "$vite_bin" ]; then
    echo "未发现 Vite，可执行文件缺失：$vite_bin"
    echo "在 $dir 重新安装依赖 (包含 devDependencies)..."
    (cd "$dir" && npm install)
  fi
  if [ ! -x "$vite_bin" ]; then
    echo -e "${RED}✗ 依赖安装后仍未找到 Vite: $vite_bin${NC}"
    echo "请检查网络代理或手动执行: (cd $dir && npm install)"
    exit 1
  fi
}

# Portal Frontend
echo "启动 Portal Frontend..."
ensure_node_modules "portal/frontend"
cd portal/frontend
npm run dev -- --host 0.0.0.0 --port "${PORTAL_FE_PORT}" > ../../logs/portal-frontend.log 2>&1 &
PORTAL_PID=$!
cd ../..
wait_for_http "Portal Frontend" "http://localhost:${PORTAL_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ Portal Frontend 运行中 (PID: $PORTAL_PID, Port: ${PORTAL_FE_PORT})${NC}"

# System Frontend
echo "启动 System Frontend..."
ensure_node_modules "system/frontend"
cd system/frontend
npm run dev -- --host 0.0.0.0 --port "${SYSTEM_FE_PORT}" > ../../logs/system-frontend.log 2>&1 &
SYSTEM_FE_PID=$!
cd ../..
wait_for_http "System Frontend" "http://localhost:${SYSTEM_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ System Frontend 运行中 (PID: $SYSTEM_FE_PID, Port: ${SYSTEM_FE_PORT})${NC}"

# Manager Frontend
echo "启动 Manager Frontend..."
ensure_node_modules "manager/frontend"
cd manager/frontend
npm run dev -- --host 0.0.0.0 --port "${MANAGER_FE_PORT}" > ../../logs/manager-frontend.log 2>&1 &
MANAGER_FE_PID=$!
cd ../..
wait_for_http "Manager Frontend" "http://localhost:${MANAGER_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ Manager Frontend 运行中 (PID: $MANAGER_FE_PID, Port: ${MANAGER_FE_PORT})${NC}"

# Meta Frontend
echo "启动 Meta Frontend..."
ensure_node_modules "meta/frontend"
cd meta/frontend
npm run dev -- --host 0.0.0.0 --port "${META_FE_PORT}" > ../../logs/meta-frontend.log 2>&1 &
META_FE_PID=$!
cd ../..
wait_for_http "Meta Frontend" "http://localhost:${META_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ Meta Frontend 运行中 (PID: $META_FE_PID, Port: ${META_FE_PORT})${NC}"

# Transfer Frontend
echo "启动 Transfer Frontend..."
ensure_node_modules "transfer/frontend"
cd transfer/frontend
npm run dev -- --host 0.0.0.0 --port "${TRANSFER_FE_PORT}" > ../../logs/transfer-frontend.log 2>&1 &
TRANSFER_FE_PID=$!
cd ../..
wait_for_http "Transfer Frontend" "http://localhost:${TRANSFER_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ Transfer Frontend 运行中 (PID: $TRANSFER_FE_PID, Port: ${TRANSFER_FE_PORT})${NC}"

# 保存前端 PIDs
echo $PORTAL_PID > .dev-pids/portal-frontend.pid
echo $SYSTEM_FE_PID > .dev-pids/system-frontend.pid
echo $MANAGER_FE_PID > .dev-pids/manager-frontend.pid
echo $META_FE_PID > .dev-pids/meta-frontend.pid
echo $TRANSFER_FE_PID > .dev-pids/transfer-frontend.pid

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ 所有后端服务启动完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "服务地址:"
echo "  Portal FE:    http://localhost:${PORTAL_FE_PORT}"
echo "  Gateway:  http://localhost:8000"
echo "  System:   http://localhost:8080"
echo "  Manager:  http://localhost:8081"
echo "  Meta:     http://localhost:8082"
echo "  Transfer: http://localhost:8083"
echo "  System FE:    http://localhost:${SYSTEM_FE_PORT}"
echo "  Manager FE:   http://localhost:${MANAGER_FE_PORT}"
echo "  Meta FE:      http://localhost:${META_FE_PORT}"
echo "  Transfer FE:  http://localhost:${TRANSFER_FE_PORT}"
echo ""
echo "进程 PID:"
echo "  System:   $SYSTEM_PID"
echo "  Manager:  $MANAGER_PID"
echo "  Meta:     $META_PID"
echo "  Transfer: $TRANSFER_PID"
echo "  Transfer Worker: $TRANSFER_WORKER_PID"
echo "  Gateway:  $GATEWAY_PID"
echo ""
echo "日志文件:"
echo "  System:   logs/system-backend.log"
echo "  Manager:  logs/manager-backend.log"
echo "  Meta:     logs/meta-backend.log"
echo "  Transfer: logs/transfer-backend.log"
echo "  Transfer Worker: logs/transfer-worker.log"
echo "  Gateway:  logs/gateway.log"
echo "  Meta FE:  logs/meta-frontend.log"
echo "  Transfer FE:  logs/transfer-frontend.log"
echo ""
echo "停止所有服务: make dev-stop 或 ./scripts/dev-stop.sh"
echo ""

# 保存 PIDs 到文件以便停止时使用
mkdir -p .dev-pids
echo $SYSTEM_PID > .dev-pids/system.pid
echo $MANAGER_PID > .dev-pids/manager.pid
echo $META_PID > .dev-pids/meta.pid
echo $TRANSFER_PID > .dev-pids/transfer.pid
echo $TRANSFER_WORKER_PID > .dev-pids/transfer-worker.pid
echo $GATEWAY_PID > .dev-pids/gateway.pid
