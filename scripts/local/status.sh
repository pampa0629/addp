#!/bin/bash
# ADDP Local Service Status Checker

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

PID_DIR="${ROOT_DIR}/.local-pids"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}📊 ADDP Local Services Status${NC}"
echo ""

SERVICES=("system" "manager" "meta" "transfer" "orchestrator" "develop" "gateway")

for service in "${SERVICES[@]}"; do
    pid_file="${PID_DIR}/${service}.pid"

    if [ ! -f "$pid_file" ]; then
        echo -e "${RED}❌ ${service}: Not running (no PID file)${NC}"
        continue
    fi

    pid=$(cat "$pid_file")

    if kill -0 "$pid" 2>/dev/null; then
        # Check memory usage
        if [[ "$OSTYPE" == "darwin"* ]]; then
            mem=$(ps -o rss= -p $pid | awk '{printf "%.1fMB", $1/1024}')
        else
            mem=$(ps -o rss= -p $pid | awk '{printf "%.1fMB", $1/1024}')
        fi
        echo -e "${GREEN}✅ ${service}: Running (PID: ${pid}, MEM: ${mem})${NC}"
    else
        echo -e "${RED}❌ ${service}: Dead (stale PID: ${pid})${NC}"
    fi
done

echo ""
