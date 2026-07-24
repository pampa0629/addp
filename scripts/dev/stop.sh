#!/bin/bash

# 加载颜色定义
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/../utils/colors.sh"

echo "🛑 停止 ADDP 开发环境"

# 卸载由 launchd KeepAlive 托管的当前工作区服务，避免进程被杀死后立即重启。
stop_workspace_launchd_jobs() {
  if [ "$(uname -s)" != "Darwin" ] || ! command -v launchctl >/dev/null 2>&1; then
    return 0
  fi

  local domain="gui/$(id -u)"
  local labels
  local cleanup_failed=false
  labels=$(launchctl list 2>/dev/null | awk '$3 ~ /^com\.addp\.codex\./ {print $3}' || true)

  if [ -z "$labels" ]; then
    return 0
  fi

  while IFS= read -r label; do
    [ -n "$label" ] || continue

    local service_target="${domain}/${label}"
    local job_info
    job_info=$(launchctl print "$service_target" 2>/dev/null || true)

    case "$job_info" in
      *"$ROOT_DIR/"*) ;;
      *) continue ;;
    esac

    echo "  卸载 launchd 托管作业: $label"
    if launchctl bootout "$service_target" >/dev/null 2>&1; then
      continue
    fi

    # 作业可能在 list 和 bootout 之间自行退出；仅在仍存在时视为失败。
    if launchctl print "$service_target" >/dev/null 2>&1; then
      echo -e "${RED}  ✗ 无法卸载 launchd 托管作业: $label${NC}"
      cleanup_failed=true
    fi
  done <<< "$labels"

  if [ "$cleanup_failed" = true ]; then
    return 1
  fi
}

# ============================================================
# 并发停止函数
# ============================================================
stop_services_concurrent() {
  echo -e "${YELLOW}并发停止所有服务...${NC}"

  local launchd_cleanup_failed=false
  if ! stop_workspace_launchd_jobs; then
    launchd_cleanup_failed=true
  fi

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
    echo -e "${YELLOW}⚠️  未找到 PID 文件,将执行兜底清理${NC}"
  else
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
        break
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
  fi

  # Phase 5: 兜底清理 pkill（清理可能的残留进程）
  # 清理 go run 进程
  pkill -9 -f "go run cmd/server/main.go" 2>/dev/null || true
  pkill -9 -f "go run cmd/worker/main.go" 2>/dev/null || true
  pkill -9 -f "go run cmd/gateway/main.go" 2>/dev/null || true
  # 清理二进制进程（新方式）
  pkill -9 -f "addp-system" 2>/dev/null || true
  pkill -9 -f "addp-manager" 2>/dev/null || true
  pkill -9 -f "addp-meta" 2>/dev/null || true
  pkill -9 -f "addp-transfer" 2>/dev/null || true
  pkill -9 -f "addp-orchestrator" 2>/dev/null || true
  pkill -9 -f "addp-develop" 2>/dev/null || true
  pkill -9 -f "addp-service" 2>/dev/null || true
  pkill -9 -f "addp-monitor" 2>/dev/null || true
  pkill -9 -f "addp-standard" 2>/dev/null || true
  pkill -9 -f "addp-model" 2>/dev/null || true
  pkill -9 -f "addp-quality" 2>/dev/null || true
  pkill -9 -f "addp-asset" 2>/dev/null || true
  pkill -9 -f "addp-portal" 2>/dev/null || true
  pkill -9 -f "addp-graph" 2>/dev/null || true
  pkill -9 -f "addp-gateway" 2>/dev/null || true
  pkill -9 -f "addp-.*-worker" 2>/dev/null || true
  # 清理前端和 Python 进程
  pkill -9 -f "vite" 2>/dev/null || true
  pkill -9 -f "engines/python-workflow.*api_server.py" 2>/dev/null || true
  pkill -9 -f "engines/model3d-workflow.*api_server.py" 2>/dev/null || true
  pkill -9 -f "engines/pointcloud-workflow.*api_server.py" 2>/dev/null || true
  pkill -9 -f "engines/spark-workflow.*api_server.py" 2>/dev/null || true
  pkill -9 -f "engines/jupyter.*api_server.py" 2>/dev/null || true
  pkill -9 -f "manager/raster-mosaic-runtime.*app.py" 2>/dev/null || true
  pkill -9 -f "jupyter.*lab.*8088" 2>/dev/null || true
  pkill -9 -f "python.*main.py.*copilot" 2>/dev/null || true
  pkill -9 -f "agent/backend/main.py" 2>/dev/null || true
  pkill -9 -f "uvicorn main:app.*8087" 2>/dev/null || true
  docker rm -f pointcloud-workflow-engine >/dev/null 2>&1 || true
  docker rm -f supermap-workflow-engine >/dev/null 2>&1 || true

  # Phase 6: 按端口清理残留进程（处理手动启动的进程）
  echo -e "${YELLOW}检查端口占用...${NC}"
  for port in 8180 8081 8082 8083 8084 8185 8086 8087 8088 8089 8097 8098 8099 8100 8101 8102 8103 8110 8181 8182 8183 8184 8186 8190 8291 5170 5173 5174 5175 5176 5177 5178 5179 5180 5181 5182 5183 5184 5185 5186 5187; do
    pid=$(lsof -ti :$port 2>/dev/null || true)
    if [ -n "$pid" ]; then
      # 获取进程的命令行信息
      proc_cmd=$(ps -p $pid -o command= 2>/dev/null || echo "")

      # 检查是否是 ADDP 相关进程（Go 二进制、vite、python workflow/copilot/agent、jupyter）
      if echo "$proc_cmd" | grep -qE "(addp-|go run|vite|api_server\.py|manager/raster-mosaic-runtime.*app\.py|uvicorn|jupyter.*lab|agent/backend/main\.py|copilot/backend/main\.py)"; then
        echo "  发现端口 $port 被 ADDP 进程占用 (PID: $pid)，强制清理..."
        echo "    进程: $(echo "$proc_cmd" | cut -c1-80)"
        kill -9 $pid 2>/dev/null || true
      else
        echo -e "${YELLOW}  ⚠️  端口 $port 被非 ADDP 进程占用 (PID: $pid)，跳过清理${NC}"
        echo "    进程: $(echo "$proc_cmd" | cut -c1-80)"
        echo "    如需使用此端口，请手动关闭该进程或修改 ADDP 端口配置"
      fi
    fi
  done

  # Phase 7: 等待端口释放（避免 restart 时端口冲突）
  echo -e "${YELLOW}等待端口释放...${NC}"
  sleep 2

  if [ "$launchd_cleanup_failed" = true ]; then
    echo -e "${RED}✗ launchd 托管作业未完全卸载，停止流程失败${NC}"
    return 1
  fi

  echo -e "${GREEN}✓ 清理完成${NC}"
}

# ============================================================
# 执行并发停止
# ============================================================
STOP_STATUS=0
stop_services_concurrent || STOP_STATUS=$?

# 清理 Vite 缓存（避免旧代码被缓存）
echo ""
echo -e "${YELLOW}清理前端缓存...${NC}"

for frontend_dir in "$ROOT_DIR/console/frontend" "$ROOT_DIR/system/frontend" "$ROOT_DIR/manager/frontend" "$ROOT_DIR/meta/frontend" "$ROOT_DIR/transfer/frontend" "$ROOT_DIR/orchestrator/frontend" "$ROOT_DIR/develop/frontend"; do
  if [ -d "$frontend_dir" ]; then
    rm -rf "$frontend_dir/node_modules/.vite" "$frontend_dir/.vite" 2>/dev/null || true
  fi
done
echo "✓ 前端缓存已清理"

# 清理 go run 的临时文件（避免旧二进制被使用）
echo ""
echo -e "${YELLOW}清理 go run 临时文件...${NC}"
# 清理 go run 的两级缓存:
# 1. 临时目录缓存
rm -rf /tmp/go-build* 2>/dev/null || true
rm -rf /var/folders/*/T/go-build* 2>/dev/null || true
# 2. 用户级缓存（~/Library/Caches/go-build 中的二进制）
# 注意：不清理编译缓存本身，只清理 go run 生成的可执行文件
find "$HOME/Library/Caches/go-build" -name "main" -type f -mtime -1 -delete 2>/dev/null || true
echo "✓ go run 临时文件已清理"

# 保留开发二进制文件（加速重启）
# 如需清理二进制，请手动删除: rm -rf .dev-bins

# 清理 PID 文件
if [ -d ".dev-pids" ]; then
  rm -rf .dev-pids
  echo "✓ PID 文件已清理"
fi

echo ""
if [ "$STOP_STATUS" -ne 0 ]; then
  echo -e "${RED}========================================${NC}"
  echo -e "${RED}✗ 服务停止未完成，请先处理上述错误${NC}"
  echo -e "${RED}========================================${NC}"
  exit "$STOP_STATUS"
fi

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ 所有服务已停止并清理完成${NC}"
echo -e "${GREEN}========================================${NC}"
