#!/bin/bash
# ADDP Local Service Restarter
# 重启单个或所有服务

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SERVICE=$1

# Colors
BLUE='\033[0;34m'
NC='\033[0m'

if [ -z "$SERVICE" ]; then
    echo -e "${BLUE}🔄 Restarting all services...${NC}"
    "$SCRIPT_DIR/stop.sh"
    sleep 2
    "$SCRIPT_DIR/start.sh"
else
    echo -e "${BLUE}🔄 Restarting ${SERVICE}...${NC}"

    PID_DIR="${SCRIPT_DIR}/../../.local-pids"
    PID_FILE="${PID_DIR}/${SERVICE}.pid"

    # Stop service
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if kill -0 "$PID" 2>/dev/null; then
            echo "Stopping ${SERVICE} (PID: ${PID})..."
            kill "$PID"
            sleep 2
        fi
        rm -f "$PID_FILE"
    fi

    # Restart all to maintain dependency order
    echo "Restarting all services to maintain dependency order..."
    "$SCRIPT_DIR/stop.sh"
    sleep 1
    "$SCRIPT_DIR/start.sh"
fi
