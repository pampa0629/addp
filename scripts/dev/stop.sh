#!/bin/bash

# 加载颜色定义
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/../utils/colors.sh"

cd "${ROOT_DIR}"
source "${SCRIPT_DIR}/lifecycle-lock.sh"
source "${SCRIPT_DIR}/ports.sh"
addp_acquire_lifecycle_lock stop "$@"

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

addp_dev_pid_owned_by_workspace() {
  local pid="$1" cwd
  cwd=$(lsof -nP -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)
  case "$cwd" in
    "$ROOT_DIR"|"$ROOT_DIR"/*) return 0 ;;
  esac
  return 1
}

# 只清理当前工作区端口上、工作目录也属于当前工作区的残留监听者。
stop_port_listeners() {
  local ports='' name variable preferred saved listener_pids pid proc_cmd
  while read -r name variable preferred; do
    saved=$(addp_dev_saved_port "$variable")
    ports="${ports:+${ports},}${saved:-${!variable:-$preferred}}"
  done < <(addp_dev_port_specs)
  listener_pids=$(lsof -nP -a -iTCP:"$ports" -sTCP:LISTEN -Fp 2>/dev/null |
    sed -n 's/^p\([0-9][0-9]*\)$/\1/p' | sort -u)

  for pid in $listener_pids; do
    proc_cmd=$(ps -p "$pid" -o command= 2>/dev/null) || continue
    [ -n "$proc_cmd" ] || continue
    if addp_dev_pid_owned_by_workspace "$pid"; then
      echo "  发现工作区残留监听进程 (PID: $pid)，强制清理..."
      echo "    进程: $(echo "$proc_cmd" | cut -c1-80)"
      kill -9 "$pid" 2>/dev/null || true
    else
      echo -e "${YELLOW}  ⚠️  端口被其他工作区进程监听 (PID: $pid)，跳过清理${NC}"
    fi
  done
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
        if [ -n "$pid" ] && ps -p "$pid" > /dev/null 2>&1 && addp_dev_pid_owned_by_workspace "$pid"; then
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

  # Phase 5: 仅清理带当前工作区标签的 Runtime 容器。
  for name in pointcloud-workflow document-workflow geopython-workflow supermap-workflow; do
    local container="${name}-engine" labels
    labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || continue
    if [ "$labels" = "addp-runtimes|${container}|${ROOT_DIR}" ]; then
      docker rm -f "$container" >/dev/null 2>&1 || true
    fi
  done

  # Phase 6: 批量检查监听端口并清理残留进程（处理手动启动的进程）
  echo -e "${YELLOW}检查端口占用...${NC}"
  stop_port_listeners

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
