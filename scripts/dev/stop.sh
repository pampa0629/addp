#!/bin/bash

echo "🛑 停止 ADDP 开发环境"

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# ============================================================
# 并发停止函数
# ============================================================
stop_services_concurrent() {
  echo -e "${YELLOW}并发停止所有服务...${NC}"

  # Phase 1: 收集所有 PIDs
  local all_pids=()
  local pid_names=()

  if [ -d ".dev-pids" ]; then
    for pidfile in .dev-pids/*.pid; do
      if [ -f "$pidfile" ]; then
        pid=$(cat "$pidfile" 2>/dev/null)
        if [ -n "$pid" ] && ps -p "$pid" > /dev/null 2>&1; then
          all_pids+=("$pid")
          service_name=$(basename "$pidfile" .pid)
          pid_names+=("$service_name")
          echo "  发现进程: $service_name (PID: $pid)"
        fi
      fi
    done
  fi

  if [ ${#all_pids[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ 没有运行中的服务${NC}"
    return 0
  fi

  echo ""
  echo "发现 ${#all_pids[@]} 个进程，发送 TERM 信号..."

  # Phase 2: 并发发送 TERM 信号（不等待）
  for pid in "${all_pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done

  # Phase 3: 统一等待（最多 10 次重试，共 5 秒）
  echo -e "${YELLOW}等待进程优雅退出（最多 5 秒）...${NC}"
  for i in {1..10}; do
    local remaining=0
    for pid in "${all_pids[@]}"; do
      if ps -p "$pid" > /dev/null 2>&1; then
        ((remaining++))
      fi
    done

    if [ $remaining -eq 0 ]; then
      echo ""
      echo -e "${GREEN}✓ 所有服务已停止（优雅退出）${NC}"
      return 0
    fi

    echo -n "."
    sleep 0.5
  done

  echo ""

  # Phase 4: 强制杀死残留进程
  local remaining_count=0
  for pid in "${all_pids[@]}"; do
    if ps -p "$pid" > /dev/null 2>&1; then
      ((remaining_count++))
    fi
  done

  if [ $remaining_count -gt 0 ]; then
    echo -e "${YELLOW}⚠️  强制停止 ${remaining_count} 个残留进程...${NC}"
    for pid in "${all_pids[@]}"; do
      if ps -p "$pid" > /dev/null 2>&1; then
        kill -9 "$pid" 2>/dev/null || true
      fi
    done
  fi

  # Phase 5: 兜底清理 pkill（清理可能的残留进程）
  pkill -9 -f "go run cmd/server/main.go" 2>/dev/null || true
  pkill -9 -f "go run cmd/worker/main.go" 2>/dev/null || true
  pkill -9 -f "go run cmd/gateway/main.go" 2>/dev/null || true
  pkill -9 -f "vite" 2>/dev/null || true
  pkill -9 -f "python.*api_server.py" 2>/dev/null || true

  # Phase 6: 等待端口释放（避免 restart 时端口冲突）
  echo -e "${YELLOW}等待端口释放...${NC}"
  sleep 2

  echo -e "${GREEN}✓ 清理完成${NC}"
}

# ============================================================
# 执行并发停止
# ============================================================
stop_services_concurrent

# 清理 Vite 缓存（避免旧代码被缓存）
echo ""
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
  echo "✓ PID 文件已清理"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ 所有服务已停止并清理完成${NC}"
echo -e "${GREEN}========================================${NC}"
