#!/bin/bash
# ADDP Local Service Stopper
# 停止所有本地运行的服务

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

PID_DIR="${ROOT_DIR}/.local-pids"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}🛑 Stopping ADDP services (local mode)${NC}"
echo ""

stop_service() {
    local service=$1
    local pid_file="${PID_DIR}/${service}.pid"

    if [ ! -f "$pid_file" ]; then
        echo -e "${YELLOW}⚠️  ${service} not running (no PID file)${NC}"
        return 0
    fi

    local pid=$(cat "$pid_file")

    if ! kill -0 "$pid" 2>/dev/null; then
        echo -e "${YELLOW}⚠️  ${service} PID ${pid} not found (stale PID file)${NC}"
        rm -f "$pid_file"
        return 0
    fi

    echo -e "${GREEN}🛑 Stopping ${service} (PID: ${pid})...${NC}"

    # Graceful shutdown (SIGTERM)
    kill "$pid"

    # Wait up to 10 seconds
    local elapsed=0
    while kill -0 "$pid" 2>/dev/null && [ $elapsed -lt 10 ]; do
        sleep 1
        elapsed=$((elapsed + 1))
    done

    # Force kill if still running
    if kill -0 "$pid" 2>/dev/null; then
        echo -e "${YELLOW}⚠️  Force killing ${service}${NC}"
        kill -9 "$pid"
    fi

    rm -f "$pid_file"
    echo -e "${GREEN}✅ ${service} stopped${NC}"
}

# Stop in reverse order (gateway first, system last)
for service in gateway develop orchestrator transfer meta manager system; do
    stop_service "$service"
done

echo ""
echo -e "${GREEN}✅ All services stopped${NC}"

# Clean up PID directory if empty
rmdir "$PID_DIR" 2>/dev/null || true
