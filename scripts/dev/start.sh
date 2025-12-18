#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

export PROJECT_ROOT="${ROOT_DIR}"
mkdir -p logs
mkdir -p .dev-pids
mkdir -p .dev-bins

# 本地 Go 构建缓存，避免写入系统 GOPATH，并优先使用本机 Go 工具链
export GOMODCACHE="${PROJECT_ROOT}/.gomodcache"
export GOPATH="${PROJECT_ROOT}/.gopath"
export GOTOOLCHAIN="local"

# 颜色定义（提前定义，供检查函数使用）
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# ============================================================
# 服务运行状态检查函数
# ============================================================

# 检查服务是否已在运行
# 参数: $1=服务名称 $2=端口号（可选，不传则只检查PID）
# 返回: 0=未运行(可以启动), 1=已运行(跳过启动)
check_service_running() {
    local service_name=$1
    local port=$2
    local pidfile=".dev-pids/${service_name}.pid"

    # 检查 PID 文件
    if [ -f "$pidfile" ]; then
        local pid=$(cat "$pidfile" 2>/dev/null)
        if [ -n "$pid" ] && ps -p "$pid" > /dev/null 2>&1; then
            echo -e "${YELLOW}⚠️  ${service_name} 已在运行 (PID: $pid)，跳过启动${NC}"
            return 1
        fi
    fi

    # 检查端口占用（如果提供了端口参数）
    if [ -n "$port" ]; then
        if lsof -ti :$port > /dev/null 2>&1; then
            local occupying_pid=$(lsof -ti :$port)
            echo -e "${RED}✗ 端口 $port 已被占用 (PID: $occupying_pid)${NC}"
            echo -e "${RED}✗ 无法启动 ${service_name}，请先停止占用端口的进程${NC}"
            echo -e "${YELLOW}提示: 运行 'bash scripts/dev/stop.sh' 停止所有服务${NC}"
            return 1
        fi
    fi

    return 0
}

# ============================================================
# 编译函数: 快速编译服务为指定名称的二进制
# ============================================================

# 编译 Server 二进制
# 参数: $1=服务名称 $2=源码路径
build_service() {
    local name=$1
    local src_dir=$2
    local output_dir=".dev-bins"

    local binary_name="addp-${name}"
    local binary_path="${output_dir}/${binary_name}"

    # 检查是否需要重新编译（增量编译）
    if [[ -f "$binary_path" ]]; then
        local src_newer=$(find "$src_dir" -type f -name "*.go" -newer "$binary_path" 2>/dev/null | wc -l | tr -d ' ')
        if [[ $src_newer -eq 0 ]]; then
            echo "  ✓ ${binary_name} 已是最新"
            return 0
        fi
    fi

    echo "  🔨 编译 ${binary_name}..."

    (cd "$src_dir" && go build -o "${PROJECT_ROOT}/${binary_path}" cmd/server/main.go) || {
        echo "  ✗ 编译失败: ${name}"
        return 1
    }

    echo "  ✓ ${binary_name} 编译完成"
}

# 编译 Worker 二进制
# 参数: $1=服务名称 $2=源码路径
build_worker() {
    local name=$1
    local src_dir=$2
    local output_dir=".dev-bins"

    local binary_name="addp-${name}-worker"
    local binary_path="${output_dir}/${binary_name}"

    # 检查是否需要重新编译（增量编译）
    if [[ -f "$binary_path" ]]; then
        local src_newer=$(find "$src_dir" -type f -name "*.go" -newer "$binary_path" 2>/dev/null | wc -l | tr -d ' ')
        if [[ $src_newer -eq 0 ]]; then
            echo "  ✓ ${binary_name} 已是最新"
            return 0
        fi
    fi

    echo "  🔨 编译 ${binary_name}..."

    (cd "$src_dir" && go build -o "${PROJECT_ROOT}/${binary_path}" cmd/worker/main.go) || {
        echo "  ✗ 编译失败: ${name} worker"
        return 1
    }

    echo "  ✓ ${binary_name} 编译完成"
}

# 编译 Gateway 二进制
build_gateway() {
    local output_dir=".dev-bins"
    local binary_name="addp-gateway"
    local binary_path="${output_dir}/${binary_name}"

    # 检查是否需要重新编译（增量编译）
    if [[ -f "$binary_path" ]]; then
        local src_newer=$(find "gateway" -type f -name "*.go" -newer "$binary_path" 2>/dev/null | wc -l | tr -d ' ')
        if [[ $src_newer -eq 0 ]]; then
            echo "  ✓ ${binary_name} 已是最新"
            return 0
        fi
    fi

    echo "  🔨 编译 ${binary_name}..."

    (cd gateway && go build -o "${PROJECT_ROOT}/${binary_path}" cmd/gateway/main.go) || {
        echo "  ✗ 编译失败: gateway"
        return 1
    }

    echo "  ✓ ${binary_name} 编译完成"
}

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

# 端口配置
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

# 检查服务是否已在运行
if check_service_running "system" "8080"; then
  build_service "system" "system/backend"
  .dev-bins/addp-system > logs/system-backend.log 2> logs/system-backend-stderr.log &
  SYSTEM_PID=$!
  echo $SYSTEM_PID > .dev-pids/system.pid

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
else
  # 服务已运行，从 PID 文件读取 PID
  SYSTEM_PID=$(cat .dev-pids/system.pid 2>/dev/null)
  echo -e "${GREEN}✓ System Backend 已在运行 (PID: $SYSTEM_PID)${NC}"
fi
echo ""

# 3. 并行启动所有后端服务 + Workers (System 已就绪)
echo -e "${YELLOW}Step 3/5: 并行启动所有后端服务 + Workers${NC}"

# ============================================================
# Phase 1: 并行编译所有 Go 服务
# ============================================================
echo "  [1/3] 并行编译 Backends + Workers..."

# 并行编译 6 个 Backend 服务
build_service "manager" "manager/backend" &
BUILD_PID_MANAGER=$!
build_service "meta" "meta/backend" &
BUILD_PID_META=$!
build_service "transfer" "transfer/backend" &
BUILD_PID_TRANSFER=$!
build_service "orchestrator" "orchestrator/backend" &
BUILD_PID_ORCHESTRATOR=$!
build_service "develop" "develop/backend" &
BUILD_PID_DEVELOP=$!

# 并行编译 3 个 Worker
build_worker "manager" "manager/backend" &
BUILD_PID_MANAGER_WORKER=$!
build_worker "meta" "meta/backend" &
BUILD_PID_META_WORKER=$!
build_worker "transfer" "transfer/backend" &
BUILD_PID_TRANSFER_WORKER=$!

# 等待所有编译完成
wait $BUILD_PID_MANAGER $BUILD_PID_META $BUILD_PID_TRANSFER $BUILD_PID_ORCHESTRATOR $BUILD_PID_DEVELOP $BUILD_PID_MANAGER_WORKER $BUILD_PID_META_WORKER $BUILD_PID_TRANSFER_WORKER

echo "  ${GREEN}✓ 所有服务编译完成${NC}"

# ============================================================
# Phase 2: 并行启动所有 Backend 服务
# ============================================================
echo "  [2/3] 并行启动 Backends..."

# 启动 Manager Backend（带检查）
if check_service_running "manager" "8081"; then
  .dev-bins/addp-manager > logs/manager-backend.log 2> logs/manager-backend-stderr.log &
  MANAGER_PID=$!
  echo $MANAGER_PID > .dev-pids/manager.pid
else
  MANAGER_PID=$(cat .dev-pids/manager.pid 2>/dev/null)
fi

# 启动 Meta Backend（带检查）
if check_service_running "meta" "8082"; then
  .dev-bins/addp-meta > logs/meta-backend.log 2> logs/meta-backend-stderr.log &
  META_PID=$!
  echo $META_PID > .dev-pids/meta.pid
else
  META_PID=$(cat .dev-pids/meta.pid 2>/dev/null)
fi

# 启动 Transfer Backend（带检查）
if check_service_running "transfer" "8083"; then
  .dev-bins/addp-transfer > logs/transfer-backend.log 2> logs/transfer-backend-stderr.log &
  TRANSFER_PID=$!
  echo $TRANSFER_PID > .dev-pids/transfer.pid
else
  TRANSFER_PID=$(cat .dev-pids/transfer.pid 2>/dev/null)
fi

# 启动 Orchestrator Backend（带检查）
if check_service_running "orchestrator" "8084"; then
  .dev-bins/addp-orchestrator > logs/orchestrator-backend.log 2> logs/orchestrator-backend-stderr.log &
  ORCHESTRATOR_PID=$!
  echo $ORCHESTRATOR_PID > .dev-pids/orchestrator.pid
else
  ORCHESTRATOR_PID=$(cat .dev-pids/orchestrator.pid 2>/dev/null)
fi

# 启动 Develop Backend（带检查）
if check_service_running "develop" "8085"; then
  .dev-bins/addp-develop > logs/develop-backend.log 2> logs/develop-backend-stderr.log &
  DEVELOP_PID=$!
  echo $DEVELOP_PID > .dev-pids/develop.pid
else
  DEVELOP_PID=$(cat .dev-pids/develop.pid 2>/dev/null)
fi

# 并行启动 3 个 Workers
if check_service_running "manager-worker" ""; then
  .dev-bins/addp-manager-worker > logs/manager-worker.log 2>&1 &
  MANAGER_WORKER_PID=$!
  echo $MANAGER_WORKER_PID > .dev-pids/manager-worker.pid
else
  MANAGER_WORKER_PID=$(cat .dev-pids/manager-worker.pid 2>/dev/null)
fi

if check_service_running "meta-worker" ""; then
  .dev-bins/addp-meta-worker > logs/meta-worker.log 2>&1 &
  META_WORKER_PID=$!
  echo $META_WORKER_PID > .dev-pids/meta-worker.pid
else
  META_WORKER_PID=$(cat .dev-pids/meta-worker.pid 2>/dev/null)
fi

if check_service_running "transfer-worker" ""; then
  .dev-bins/addp-transfer-worker > logs/transfer-worker.log 2>&1 &
  TRANSFER_WORKER_PID=$!
  echo $TRANSFER_WORKER_PID > .dev-pids/transfer-worker.pid
else
  TRANSFER_WORKER_PID=$(cat .dev-pids/transfer-worker.pid 2>/dev/null)
fi

echo "  ${GREEN}✓ 所有服务已启动，等待健康检查...${NC}"

# ============================================================
# Phase 3: 并行等待所有 Backends 健康检查
# ============================================================
echo "  [3/3] 并行健康检查..."

# 并发等待 Manager Backend
(
  WAIT_COUNT=0
  until curl -f http://localhost:8081/health > /dev/null 2>&1; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e "${RED}✗ Manager Backend 启动超时${NC}"
      echo "查看日志: tail -f logs/manager-backend.log"
      exit 1
    fi
  done
) &
MANAGER_WAIT_PID=$!

# 并发等待 Meta Backend
(
  WAIT_COUNT=0
  until curl -f http://localhost:8082/health > /dev/null 2>&1; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e "${RED}✗ Meta Backend 启动超时${NC}"
      echo "查看日志: tail -f logs/meta-backend.log"
      exit 1
    fi
  done
) &
META_WAIT_PID=$!

# 并发等待 Transfer Backend
(
  WAIT_COUNT=0
  until curl -f http://localhost:8083/health > /dev/null 2>&1; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e "${RED}✗ Transfer Backend 启动超时${NC}"
      echo "查看日志: tail -f logs/transfer-backend-stderr.log"
      exit 1
    fi
  done
) &
TRANSFER_WAIT_PID=$!

# 并发等待 Orchestrator Backend
(
  WAIT_COUNT=0
  until curl -f http://localhost:8084/health > /dev/null 2>&1; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e "${RED}✗ Orchestrator Backend 启动超时${NC}"
      echo "查看日志: tail -f logs/orchestrator-backend-stderr.log"
      exit 1
    fi
  done
) &
ORCHESTRATOR_WAIT_PID=$!

# 并发等待 Develop Backend
(
  WAIT_COUNT=0
  until curl -f http://localhost:8085/health > /dev/null 2>&1; do
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e "${RED}✗ Develop Backend 启动超时${NC}"
      echo "查看日志: tail -f logs/develop-backend.log"
      exit 1
    fi
  done
) &
DEVELOP_WAIT_PID=$!

# 等待所有并发的 health check 完成
wait $MANAGER_WAIT_PID $META_WAIT_PID $TRANSFER_WAIT_PID $ORCHESTRATOR_WAIT_PID $DEVELOP_WAIT_PID

echo ""
echo -e "${GREEN}✓ 所有 Backends + Workers 全部就绪${NC}"
echo "  Manager Backend:    PID $MANAGER_PID (http://localhost:8081)"
echo "  Meta Backend:       PID $META_PID (http://localhost:8082)"
echo "  Transfer Backend:   PID $TRANSFER_PID (http://localhost:8083)"
echo "  Orchestrator Backend: PID $ORCHESTRATOR_PID (http://localhost:8084)"
echo "  Develop Backend:    PID $DEVELOP_PID (http://localhost:8085)"
echo "  Manager Worker:     PID $MANAGER_WORKER_PID"
echo "  Meta Worker:        PID $META_WORKER_PID"
echo "  Transfer Worker:    PID $TRANSFER_WORKER_PID"
echo ""

# ============================================================
# Step 4: Start GeoPandas Engine (Python service)
# ============================================================
echo -e "${YELLOW}Step 4/5: 启动 GeoPandas Engine...${NC}"

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
if [ ! -d "engines/geopandas/venv" ]; then
    echo "首次启动，创建 Python 虚拟环境..."
    cd engines/geopandas
    python3 -m venv venv
    NEED_INSTALL=true
else
    # 检查关键依赖是否已安装
    if ! ./engines/geopandas/venv/bin/python -c "import flask" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd engines/geopandas
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
if check_service_running "geopandas-engine" "8099"; then
  echo "启动 GeoPandas Engine..."
  cd engines/geopandas

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
  ./venv/bin/python api_server.py > ../../logs/geopandas-engine.log 2> ../../logs/geopandas-engine-stderr.log &
  GEOPANDAS_PID=$!
  echo $GEOPANDAS_PID > ../../.dev-pids/geopandas-engine.pid
  cd ../..

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
else
  GEOPANDAS_PID=$(cat .dev-pids/geopandas-engine.pid 2>/dev/null)
  echo -e "${GREEN}✓ GeoPandas Engine 已在运行 (PID: $GEOPANDAS_PID)${NC}"
fi
echo ""

# 5. 启动 Gateway
echo -e "${YELLOW}Step 5/5: 启动 Gateway${NC}"

if check_service_running "gateway" "8000"; then
  build_gateway
  # 重置 PORT 环境变量为 Gateway 的端口
  export PORT=8000
  .dev-bins/addp-gateway > logs/gateway.log 2> logs/gateway-stderr.log &
  GATEWAY_PID=$!
  echo $GATEWAY_PID > .dev-pids/gateway.pid

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
else
  GATEWAY_PID=$(cat .dev-pids/gateway.pid 2>/dev/null)
  echo -e "${GREEN}✓ Gateway 已在运行 (PID: $GATEWAY_PID)${NC}"
fi
echo ""

# 6. 启动前端服务（保持原有并行逻辑）
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

# ============================================================
# 并发启动所有前端服务（Bash 3.2 兼容）
# ============================================================
echo -e "${YELLOW}Step 8/8: 并发启动所有前端服务 (Portal + 6 个模块)${NC}"

# 定义前端配置（格式：名称:端口:目录）
# 使用普通数组而非关联数组（兼容 Bash 3.2）
FRONTEND_CONFIGS=(
  "portal:${PORTAL_FE_PORT}:portal/frontend"
  "system:${SYSTEM_FE_PORT}:system/frontend"
  "manager:${MANAGER_FE_PORT}:manager/frontend"
  "meta:${META_FE_PORT}:meta/frontend"
  "transfer:${TRANSFER_FE_PORT}:transfer/frontend"
  "orchestrator:${ORCHESTRATOR_FE_PORT}:orchestrator/frontend"
  "develop:${DEVELOP_FE_PORT}:develop/frontend"
)

echo "并发启动所有前端..."

# 存储 PIDs（使用临时文件）
FRONTEND_PID_FILE="/tmp/addp-frontend-pids-$$"
> "$FRONTEND_PID_FILE"  # 清空文件

# 并发启动所有前端
for config in "${FRONTEND_CONFIGS[@]}"; do
  IFS=':' read -r name port dir <<< "$config"

  # 检查前端服务是否已在运行
  if check_service_running "${name}-frontend" "$port"; then
    (
      ensure_node_modules "$dir"
      cd "$dir"
      npm run dev -- --host 0.0.0.0 --port "$port" > "../../logs/${name}-frontend.log" 2>&1
    ) &

    pid=$!
    echo "${name}:${pid}" >> "$FRONTEND_PID_FILE"
    echo "  启动 ${name} Frontend (PID: $pid, Port: $port)"
  else
    # 服务已运行，从 PID 文件读取
    existing_pid=$(cat ".dev-pids/${name}-frontend.pid" 2>/dev/null)
    if [ -n "$existing_pid" ]; then
      echo "${name}:${existing_pid}" >> "$FRONTEND_PID_FILE"
    fi
  fi
done

echo ""
echo "并发等待所有前端就绪..."

# 并发等待所有前端的健康检查
HEALTH_CHECK_PIDS=()
for config in "${FRONTEND_CONFIGS[@]}"; do
  IFS=':' read -r name port dir <<< "$config"

  (
    WAIT_COUNT=0
    until curl -fsS "http://localhost:${port}" > /dev/null 2>&1; do
      sleep 1
      WAIT_COUNT=$((WAIT_COUNT + 1))
      if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
        echo -e "${RED}✗ ${name} Frontend 启动超时 (Port: ${port})${NC}"
        echo "查看日志: tail -f logs/${name}-frontend.log"
        exit 1
      fi
    done
    # 从临时文件中查找 PID
    pid=$(grep "^${name}:" "$FRONTEND_PID_FILE" | cut -d: -f2)
    echo -e "${GREEN}✓ ${name} Frontend 就绪 (PID: ${pid}, Port: ${port})${NC}"
  ) &
  HEALTH_CHECK_PIDS+=($!)
done

# 只等待健康检查进程完成（不等待前端 npm 进程）
for pid in "${HEALTH_CHECK_PIDS[@]}"; do
  wait "$pid"
done

echo ""
echo -e "${GREEN}✓ 所有前端服务已启动${NC}"

# 保存前端 PIDs 到 .dev-pids 目录
while IFS=: read -r name pid; do
  echo "$pid" > ".dev-pids/${name}-frontend.pid"
done < "$FRONTEND_PID_FILE"

# 为了兼容性，设置这些变量（从临时文件读取）
PORTAL_PID=$(grep "^portal:" "$FRONTEND_PID_FILE" | cut -d: -f2)
SYSTEM_FE_PID=$(grep "^system:" "$FRONTEND_PID_FILE" | cut -d: -f2)
MANAGER_FE_PID=$(grep "^manager:" "$FRONTEND_PID_FILE" | cut -d: -f2)
META_FE_PID=$(grep "^meta:" "$FRONTEND_PID_FILE" | cut -d: -f2)
TRANSFER_FE_PID=$(grep "^transfer:" "$FRONTEND_PID_FILE" | cut -d: -f2)
ORCHESTRATOR_FE_PID=$(grep "^orchestrator:" "$FRONTEND_PID_FILE" | cut -d: -f2)
DEVELOP_FE_PID=$(grep "^develop:" "$FRONTEND_PID_FILE" | cut -d: -f2)

# 清理临时文件
rm -f "$FRONTEND_PID_FILE"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ ADDP 开发环境启动完成！${NC}"
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
echo "后端服务 PID:"
echo "  System Backend:       $SYSTEM_PID"
echo "  Manager Backend:      $MANAGER_PID"
echo "  Meta Backend:         $META_PID"
echo "  Transfer Backend:     $TRANSFER_PID"
echo "  Orchestrator Backend: $ORCHESTRATOR_PID"
echo "  Develop Backend:      $DEVELOP_PID"
echo "  GeoPandas Engine:     $GEOPANDAS_PID"
echo "  Gateway:              $GATEWAY_PID"
echo ""
echo "Workers PID:"
echo "  Manager Worker:       $MANAGER_WORKER_PID"
echo "  Meta Worker:          $META_WORKER_PID"
echo "  Transfer Worker:      $TRANSFER_WORKER_PID"
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
