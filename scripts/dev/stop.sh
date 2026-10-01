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

  local phase="$1" is_system
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

    is_system=false
    case "$job_info" in
      *"$ROOT_DIR/.dev-bins/addp-system"$'\n'*|*"$ROOT_DIR/.dev-bins/addp-system "*|*"$ROOT_DIR/.dev-bins/addp-system") is_system=true ;;
    esac
    if [ "$phase" = system ]; then
      [ "$is_system" = true ] || continue
    else
      [ "$is_system" = false ] || continue
    fi

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

# PID 文件和残留监听者合并后，使用相同的分阶段停止流程。
add_workspace_stop_pid() {
  local pid="$1" known proc_cmd
  [[ "$pid" =~ ^[0-9]+$ ]] || return 0
  for known in "${all_pids[@]}" "${system_pids[@]}"; do
    [ "$pid" != "$known" ] || return 0
  done
  proc_cmd=$(ps -p "$pid" -o command= 2>/dev/null) || return 0
  if ! addp_dev_pid_owned_by_workspace "$pid"; then
    echo -e "${YELLOW}  ⚠️  跳过其他工作区进程 (PID: $pid)${NC}"
    return 0
  fi
  case "$proc_cmd" in
    "$ROOT_DIR/.dev-bins/addp-system"|"$ROOT_DIR/.dev-bins/addp-system "*|.dev-bins/addp-system|.dev-bins/addp-system\ *|./.dev-bins/addp-system|./.dev-bins/addp-system\ *) system_pids+=("$pid") ;;
    *) all_pids+=("$pid") ;;
  esac
  echo "  发现工作区进程 (PID: $pid): ${proc_cmd:0:100}"
}

collect_workspace_listeners() {
  local ports='' name variable preferred saved listener_pids pid
  while read -r name variable preferred; do
    saved=$(addp_dev_saved_port "$variable")
    ports="${ports:+${ports},}${saved:-${!variable:-$preferred}}"
  done < <(addp_dev_port_specs)
  listener_pids=$(lsof -nP -a -iTCP:"$ports" -sTCP:LISTEN -Fp 2>/dev/null |
    sed -n 's/^p\([0-9][0-9]*\)$/\1/p' | sort -u)
  for pid in $listener_pids; do
    add_workspace_stop_pid "$pid"
  done
}

stop_pid_group() {
  local label="$1" pid remaining i
  shift
  [ "$#" -gt 0 ] || return 0
  echo -e "${YELLOW}停止 ${label}，等待优雅退出（最多 5 秒）...${NC}"
  for pid in "$@"; do
    if ps -p "$pid" >/dev/null 2>&1; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  for i in {1..10}; do
    remaining=0
    for pid in "$@"; do
      if ps -p "$pid" >/dev/null 2>&1; then
        remaining=$((remaining + 1))
      fi
    done
    [ "$remaining" -gt 0 ] || return 0
    sleep 0.5
  done
  for pid in "$@"; do
    if ps -p "$pid" >/dev/null 2>&1; then
      echo -e "${YELLOW}  ⚠️  $label 超时，强制停止 PID: $pid${NC}"
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
}

stop_services_concurrent() {
  local all_pids=() system_pids=() pidfile pid
  local cleanup_failed=false

  # 先收集 PID，再卸载 launchd，避免卸载后丢失进程身份。
  if [ -d .dev-pids ]; then
    for pidfile in .dev-pids/*.pid; do
      [ -f "$pidfile" ] || continue
      pid=$(cat "$pidfile" 2>/dev/null)
      add_workspace_stop_pid "$pid"
    done
  fi
  echo -e "${YELLOW}检查工作区残留监听者...${NC}"
  collect_workspace_listeners

  # 保留 System 的 HTTP 和鉴权能力，供其他实例完成注销。
  stop_workspace_launchd_jobs modules || cleanup_failed=true
  stop_pid_group "模块、Worker 和前端" "${all_pids[@]}"

  local name container labels
  for name in pointcloud-workflow document-workflow geopython-workflow supermap-workflow; do
    container="${name}-engine"
    labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || continue
    if [ "$labels" = "addp-runtimes|${container}|${ROOT_DIR}" ]; then
      if ! docker stop -t 5 "$container" >/dev/null 2>&1 ||
         ! docker rm "$container" >/dev/null 2>&1; then
        echo -e "${RED}  ✗ Runtime 容器停止失败: $container${NC}"
        cleanup_failed=true
      fi
    fi
  done

  stop_workspace_launchd_jobs system || cleanup_failed=true
  stop_pid_group "System Backend" "${system_pids[@]}"

  echo -e "${YELLOW}等待端口释放...${NC}"
  sleep 2
  if [ "$cleanup_failed" = true ]; then
    echo -e "${RED}✗ 托管作业或 Runtime 容器未完全停止${NC}"
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
