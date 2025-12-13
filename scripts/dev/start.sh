#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

export PROJECT_ROOT="${ROOT_DIR}"
mkdir -p logs
mkdir -p .dev-pids

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
ORCHESTRATOR_FE_PORT=${ORCHESTRATOR_FE_PORT:-5177}
DEVELOP_FE_PORT=${DEVELOP_FE_PORT:-5178}

# 0. 检查 Go 模块依赖
echo -e "${YELLOW}Step 0: 检查 Go 模块依赖${NC}"
echo ""

if [[ "${SKIP_MODTIDY:-0}" != "1" ]]; then
  if bash "${SCRIPT_DIR}/modtidy.sh"; then
    echo -e "${GREEN}✓ Go 依赖检查完成${NC}"
  else
    echo -e "${YELLOW}⚠️  Go 依赖检查失败,继续启动(可能需手动修复)${NC}"
  fi
else
  echo -e "${YELLOW}ℹ️  跳过 Go 依赖检查(设置了 SKIP_MODTIDY=1)${NC}"
fi
echo ""

# 1. 启动基础设施
echo -e "${YELLOW}Step 1/7: 启动基础设施（PostgreSQL, Redis, MinIO, Meilisearch）${NC}"
echo ""

# 检查基础设施是否已运行
INFRA_RUNNING=false
if docker compose -f docker-compose.infra.yml ps --status running postgres redis minio meilisearch 2>/dev/null | grep -q "Up"; then
  # 检查所有4个服务是否都在运行
  RUNNING_COUNT=$(docker compose -f docker-compose.infra.yml ps --status running postgres redis minio meilisearch 2>/dev/null | grep -c "Up" || echo "0")
  if [ "$RUNNING_COUNT" -eq 4 ]; then
    INFRA_RUNNING=true
    echo -e "${GREEN}✓ 基础设施已在运行,跳过启动${NC}"
    echo -e "${YELLOW}  (如需重启基础设施,请运行: bash scripts/infra/down.sh && bash scripts/infra/up.sh)${NC}"
  fi
fi

# 如果基础设施未完全运行,则启动
if [ "$INFRA_RUNNING" = false ]; then
  echo -e "${YELLOW}启动基础设施服务...${NC}"
  # 调用基础设施启动脚本(单一职责原则)
  if ! bash "${ROOT_DIR}/scripts/infra/up.sh"; then
    echo -e "${RED}✗ 基础设施启动失败,请检查 Docker 是否运行${NC}"
    echo -e "${YELLOW}提示: 运行 'docker info' 检查 Docker 状态${NC}"
    exit 1
  fi
  echo -e "${GREEN}✓ 基础设施启动完成${NC}"
fi

echo ""

# 2. 启动 System Backend
echo -e "${YELLOW}Step 2/7: 启动 System Backend${NC}"
cd system/backend
go run cmd/server/main.go > ../../logs/system-backend.log 2> ../../logs/system-backend-stderr.log &
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
echo -e "${YELLOW}Step 3/7: 启动 Manager Backend${NC}"
cd manager/backend
go run cmd/server/main.go > ../../logs/manager-backend.log 2> ../../logs/manager-backend-stderr.log &
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
echo -e "${YELLOW}Step 4/7: 启动 Meta Backend${NC}"
cd meta/backend
go run cmd/server/main.go > ../../logs/meta-backend.log 2> ../../logs/meta-backend-stderr.log &
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
go run cmd/server/main.go > ../../logs/transfer-backend.log 2> ../../logs/transfer-backend-stderr.log &
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

# 6.5 启动 Meta Worker
echo -e "${YELLOW}Step 6.5/7: 启动 Meta Worker${NC}"
cd meta/backend
go mod download >/dev/null 2>> ../../logs/meta-worker.log || true
go run cmd/worker/main.go > ../../logs/meta-worker.log 2>&1 &
META_WORKER_PID=$!
cd ../..
echo -e "${GREEN}✓ Meta Worker 启动中 (PID: $META_WORKER_PID)${NC}"
echo "  注意: Meta Worker 依赖 Redis，请确保 Redis 已启动"
echo ""

# 6.6 启动 Manager Worker
echo -e "${YELLOW}Step 6.6/7: 启动 Manager Worker${NC}"
cd manager/backend
go mod download >/dev/null 2>> ../../logs/manager-worker.log || true
go run cmd/worker/main.go > ../../logs/manager-worker.log 2>&1 &
MANAGER_WORKER_PID=$!
cd ../..
echo -e "${GREEN}✓ Manager Worker 启动中 (PID: $MANAGER_WORKER_PID)${NC}"
echo "  注意: Manager Worker 依赖 Redis 和 MinIO，请确保已启动"
echo ""

# 6.7 启动 Orchestrator Backend
echo -e "${YELLOW}Step 6.7/7: 启动 Orchestrator Backend${NC}"
cd orchestrator/backend
go mod download >/dev/null 2>> ../../logs/orchestrator-backend-stderr.log || true
go run cmd/server/main.go > ../../logs/orchestrator-backend.log 2> ../../logs/orchestrator-backend-stderr.log &
ORCHESTRATOR_PID=$!
cd ../..

# 等待 Orchestrator 就绪
echo "等待 Orchestrator Backend 就绪..."
WAIT_COUNT=0
until curl -f http://localhost:8084/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Orchestrator Backend 启动超时${NC}"
    echo "查看日志: tail -f logs/orchestrator-backend-stderr.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ Orchestrator Backend 就绪 (PID: $ORCHESTRATOR_PID)${NC}"
echo ""

# 6.8 启动 Develop Backend
echo -e "${YELLOW}Step 6.8/8: 启动 Develop Backend${NC}"
cd develop/backend
go mod download >/dev/null 2>> ../../logs/develop-backend-stderr.log || true
go run cmd/server/main.go > ../../logs/develop-backend.log 2> ../../logs/develop-backend-stderr.log &
DEVELOP_PID=$!
cd ../..

# 等待 Develop 就绪
echo "等待 Develop Backend 就绪..."
WAIT_COUNT=0
until curl -f http://localhost:8085/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e "${RED}✗ Develop Backend 启动超时${NC}"
    echo "查看日志: tail -f logs/develop-backend.log"
    exit 1
  fi
done
echo -e "${GREEN}✓ Develop Backend 就绪 (PID: $DEVELOP_PID)${NC}"
echo ""

# ============================================================
# Step 6.9: Start GeoPandas Engine (Python service)
# ============================================================
echo -e "${YELLOW}Step 6.9/8: 启动 GeoPandas Engine...${NC}"

# 检查 Python 3 是否安装
if ! command -v python3 &> /dev/null; then
    echo -e "${RED}✗ Python 3 未安装，请先安装 Python 3.11+${NC}"
    exit 1
fi

# 检查 Python 版本（需要 3.11+）
PYTHON_VERSION=$(python3 -c 'import sys; print(".".join(map(str, sys.version_info[:2])))')
REQUIRED_VERSION="3.11"
if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$PYTHON_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
    echo -e "${RED}✗ Python 版本过低 ($PYTHON_VERSION)，需要 3.11+${NC}"
    exit 1
fi

# 检查并创建虚拟环境（幂等）
NEED_INSTALL=false
if [ ! -d "geopandas-engine/venv" ]; then
    echo "首次启动，创建 Python 虚拟环境..."
    cd geopandas-engine
    python3 -m venv venv
    NEED_INSTALL=true
else
    # 检查关键依赖是否已安装
    if ! ./geopandas-engine/venv/bin/python -c "import flask" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd geopandas-engine
        NEED_INSTALL=true
    else
        echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    # 使用 pip 安装依赖（更稳定，避免 uv 虚拟环境识别问题）
    echo "使用 pip 安装依赖（首次安装可能需要 1-2 分钟）..."
    ./venv/bin/pip install --upgrade pip
    ./venv/bin/pip install -r requirements.txt

    # 检查安装是否成功
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Python 依赖安装完成${NC}"
    else
        echo -e "${RED}✗ Python 依赖安装失败，请检查错误信息${NC}"
        echo -e "${YELLOW}提示：某些依赖可能需要系统库支持（如 GDAL）${NC}"
        echo -e "${YELLOW}macOS: brew install gdal${NC}"
        echo -e "${YELLOW}Ubuntu: sudo apt-get install libgdal-dev${NC}"
        cd ..
        exit 1
    fi
    cd ..
fi

# 启动 GeoPandas Engine
echo "启动 GeoPandas Engine..."
cd geopandas-engine

# 设置环境变量（注意端口改为 8099）
export PORT=8099
export SYSTEM_SERVICE_URL=http://localhost:8080
export INTERNAL_API_KEY=${INTERNAL_API_KEY:-""}
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export POSTGRES_USER=addp
export POSTGRES_PASSWORD=addp_password
export POSTGRES_DB=addp
export DB_SCHEMA=develop

# 直接使用虚拟环境的 Python（无需 activate）
./venv/bin/python api_server.py > ../logs/geopandas-engine.log 2> ../logs/geopandas-engine-stderr.log &
GEOPANDAS_PID=$!
echo $GEOPANDAS_PID > ../.dev-pids/geopandas-engine.pid
cd ..

echo -e "${GREEN}✓ GeoPandas Engine 已启动 (PID: $GEOPANDAS_PID)${NC}"

# 等待健康检查通过
echo -n "等待 GeoPandas Engine 就绪..."
WAIT_COUNT=0
MAX_WAIT=60
until curl -f http://localhost:8099/health > /dev/null 2>&1; do
  echo -n "."
  sleep 1
  WAIT_COUNT=$((WAIT_COUNT + 1))
  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo -e " ${RED}✗${NC}"
    echo -e "${RED}✗ GeoPandas Engine 启动超时（60秒）${NC}"
    echo -e "${YELLOW}查看日志: tail -f logs/geopandas-engine.log${NC}"
    echo -e "${YELLOW}或检查错误: tail -f logs/geopandas-engine-stderr.log${NC}"
    exit 1
  fi
done
echo -e " ${GREEN}✓${NC}"
echo -e "${GREEN}✓ GeoPandas Engine 就绪 (http://localhost:8099)${NC}"
echo ""

# 7. 启动 Gateway
echo -e "${YELLOW}Step 7/8: 启动 Gateway${NC}"
cd gateway
# 重置 PORT 环境变量为 Gateway 的端口
export PORT=8000
go run cmd/gateway/main.go > ../logs/gateway.log 2> ../logs/gateway-stderr.log &
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

# 8. 启动前端服务
echo -e "${YELLOW}Step 8: 启动前端服务${NC}"

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

# Orchestrator Frontend
echo "启动 Orchestrator Frontend..."
ensure_node_modules "orchestrator/frontend"
cd orchestrator/frontend
npm run dev -- --host 0.0.0.0 --port "${ORCHESTRATOR_FE_PORT}" > ../../logs/orchestrator-frontend.log 2>&1 &
ORCHESTRATOR_FE_PID=$!
cd ../..
wait_for_http "Orchestrator Frontend" "http://localhost:${ORCHESTRATOR_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ Orchestrator Frontend 运行中 (PID: $ORCHESTRATOR_FE_PID, Port: ${ORCHESTRATOR_FE_PORT})${NC}"

# Develop Frontend
echo "启动 Develop Frontend..."
ensure_node_modules "develop/frontend"
cd develop/frontend
npm run dev -- --host 0.0.0.0 --port "${DEVELOP_FE_PORT}" > ../../logs/develop-frontend.log 2>&1 &
DEVELOP_FE_PID=$!
cd ../..
wait_for_http "Develop Frontend" "http://localhost:${DEVELOP_FE_PORT}" "$MAX_WAIT" || exit 1
echo -e "${GREEN}✓ Develop Frontend 运行中 (PID: $DEVELOP_FE_PID, Port: ${DEVELOP_FE_PORT})${NC}"

# 保存前端 PIDs
echo $PORTAL_PID > .dev-pids/portal-frontend.pid
echo $SYSTEM_FE_PID > .dev-pids/system-frontend.pid
echo $MANAGER_FE_PID > .dev-pids/manager-frontend.pid
echo $META_FE_PID > .dev-pids/meta-frontend.pid
echo $TRANSFER_FE_PID > .dev-pids/transfer-frontend.pid
echo $ORCHESTRATOR_FE_PID > .dev-pids/orchestrator-frontend.pid
echo $DEVELOP_FE_PID > .dev-pids/develop-frontend.pid

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
echo "  Orchestrator: http://localhost:8084"
echo "  Develop:  http://localhost:8085"
echo "  System FE:    http://localhost:${SYSTEM_FE_PORT}"
echo "  Manager FE:   http://localhost:${MANAGER_FE_PORT}"
echo "  Meta FE:      http://localhost:${META_FE_PORT}"
echo "  Transfer FE:  http://localhost:${TRANSFER_FE_PORT}"
echo "  Orchestrator FE: http://localhost:${ORCHESTRATOR_FE_PORT}"
echo "  Develop FE:   http://localhost:${DEVELOP_FE_PORT}"
echo ""
echo "进程 PID:"
echo "  System:   $SYSTEM_PID"
echo "  Manager:  $MANAGER_PID"
echo "  Meta:     $META_PID"
echo "  Transfer: $TRANSFER_PID"
echo "  Transfer Worker: $TRANSFER_WORKER_PID"
echo "  Orchestrator: $ORCHESTRATOR_PID"
echo "  Develop:  $DEVELOP_PID"
echo "  Gateway:  $GATEWAY_PID"
echo ""
echo "日志文件:"
echo "  System:   logs/system-backend.log"
echo "  Manager:  logs/manager-backend.log"
echo "  Meta:     logs/meta-backend.log"
echo "  Transfer: logs/transfer-backend.log"
echo "  Transfer Worker: logs/transfer-worker.log"
echo "  Meta Worker: logs/meta-worker.log"
echo "  Manager Worker: logs/manager-worker.log"
echo "  Orchestrator: logs/orchestrator-backend.log"
echo "  Develop:  logs/develop-backend.log"
echo "  Gateway:  logs/gateway.log"
echo "  Meta FE:  logs/meta-frontend.log"
echo "  Transfer FE:  logs/transfer-frontend.log"
echo "  Develop FE:  logs/develop-frontend.log"
echo ""
echo "停止所有服务: make dev-stop 或 ./scripts/dev/stop.sh"
echo ""

# 保存 PIDs 到文件以便停止时使用
mkdir -p .dev-pids
echo $SYSTEM_PID > .dev-pids/system.pid
echo $MANAGER_PID > .dev-pids/manager.pid
echo $META_PID > .dev-pids/meta.pid
echo $TRANSFER_PID > .dev-pids/transfer.pid
echo $TRANSFER_WORKER_PID > .dev-pids/transfer-worker.pid
echo $META_WORKER_PID > .dev-pids/meta-worker.pid
echo $MANAGER_WORKER_PID > .dev-pids/manager-worker.pid
echo $ORCHESTRATOR_PID > .dev-pids/orchestrator.pid
echo $DEVELOP_PID > .dev-pids/develop.pid
echo $GATEWAY_PID > .dev-pids/gateway.pid
