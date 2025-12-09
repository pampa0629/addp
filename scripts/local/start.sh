#!/bin/bash
# ADDP Local Service Starter
# 从预编译的 dist/ 二进制文件启动所有服务

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Configuration
PID_DIR="${ROOT_DIR}/.local-pids"
LOG_DIR="${ROOT_DIR}/logs/local"

# Detect architecture
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/arm64/aarch64/arm64/')
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
BUILD_TYPE="${BUILD_TYPE:-release}"
BUILD_NAME="${BUILD_TYPE}-${OS}-${ARCH}"

# Binary directory
BIN_DIR="${ROOT_DIR}/dist/${BUILD_NAME}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}🚀 Starting ADDP services (local mode)${NC}"
echo -e "${BLUE}📦 Using binaries from: ${BIN_DIR}${NC}"
echo ""

# Check if binaries exist
if [ ! -d "$BIN_DIR" ]; then
    echo -e "${RED}❌ Binaries not found in ${BIN_DIR}${NC}"
    echo -e "${YELLOW}Run: make build-backend-all OR ./scripts/build/compile.sh${NC}"
    exit 1
fi

# Create directories
mkdir -p "$PID_DIR" "$LOG_DIR"

# Start function
start_service() {
    local service=$1
    local port=$2
    local deps=$3  # Space-separated dependency services

    local binary="${BIN_DIR}/${service}"
    local pid_file="${PID_DIR}/${service}.pid"
    local log_file="${LOG_DIR}/${service}.log"

    # Check if already running
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            echo -e "${YELLOW}⚠️  ${service} already running (PID: ${pid})${NC}"
            return 0
        fi
    fi

    # Check dependencies
    for dep in $deps; do
        if ! check_service_health "$dep"; then
            echo -e "${RED}❌ Dependency not ready: ${dep}${NC}"
            return 1
        fi
    done

    # Check binary exists
    if [ ! -f "$binary" ]; then
        echo -e "${RED}❌ Binary not found: ${binary}${NC}"
        return 1
    fi

    # Start service
    echo -e "${GREEN}▶️  Starting ${service} on port ${port}...${NC}"

    cd "${ROOT_DIR}/${service}/backend" 2>/dev/null || cd "${ROOT_DIR}/${service}" || cd "${ROOT_DIR}"
    nohup "$binary" > "$log_file" 2>&1 &
    local pid=$!
    echo "$pid" > "$pid_file"

    # Wait for health check
    if wait_for_health "http://localhost:${port}/health" 60; then
        echo -e "${GREEN}✅ ${service} started (PID: ${pid})${NC}"
        return 0
    else
        echo -e "${RED}❌ ${service} failed to start${NC}"
        echo -e "${YELLOW}Check logs: tail -f $log_file${NC}"
        return 1
    fi
}

# Health check function
check_service_health() {
    local service=$1
    local pid_file="${PID_DIR}/${service}.pid"

    [ -f "$pid_file" ] || return 1

    local pid=$(cat "$pid_file")
    kill -0 "$pid" 2>/dev/null
}

wait_for_health() {
    local url=$1
    local timeout=$2
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done

    return 1
}

# Service startup order (system → business → gateway)
start_service "system" 8080 ""
start_service "manager" 8081 "system"
start_service "meta" 8082 "system"
start_service "transfer" 8083 "system"
start_service "orchestrator" 8084 "system"
start_service "develop" 8085 "system"
start_service "gateway" 8000 "system manager meta"

echo ""
echo -e "${GREEN}✅ All services started successfully!${NC}"
echo ""
echo "Service URLs:"
echo "  Gateway:      http://localhost:8000"
echo "  System:       http://localhost:8080"
echo "  Manager:      http://localhost:8081"
echo "  Meta:         http://localhost:8082"
echo "  Transfer:     http://localhost:8083"
echo "  Orchestrator: http://localhost:8084"
echo "  Develop:      http://localhost:8085"
echo ""
echo "Management commands:"
echo "  Status:  ./scripts/local/status.sh"
echo "  Stop:    ./scripts/local/stop.sh"
echo "  Restart: ./scripts/local/restart.sh [service]"
echo "  Logs:    tail -f logs/local/*.log"
