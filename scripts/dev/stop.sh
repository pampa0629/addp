#!/bin/bash

# 加载颜色定义
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/../utils/colors.sh"

cd "${ROOT_DIR}"
source "${SCRIPT_DIR}/lifecycle-lock.sh"
source "${SCRIPT_DIR}/ports.sh"
addp_acquire_lifecycle_lock stop "$@"

STOP_ALL=true
STOP_MODULES=()
for arg in "$@"; do
  case "$arg" in
    -h|--help)
      echo "用法: $0 [-all | -<模块名> ...]；无参数停止全部，指定模块只停止所属进程"
      exit 0 ;;
    -all) ;;
    -*)
      module="${arg#-}"
      found=false
      while read -r name variable preferred; do
        if [ "$(addp_dev_process_module "$name")" = "$module" ]; then
          found=true
          break
        fi
      done < <(addp_dev_port_specs)
      if [ "$found" != true ]; then
        echo "❌ 未知模块: $arg" >&2
        exit 1
      fi
      STOP_ALL=false
      [[ " ${STOP_MODULES[*]} " == *" $module "* ]] || STOP_MODULES+=("$module") ;;
    *) echo "❌ 未知参数: $arg" >&2; exit 1 ;;
  esac
done
if [ "$STOP_ALL" = false ] && [[ " $* " == *" -all "* ]]; then
  echo "❌ -all 不能与模块参数同时使用" >&2
  exit 1
fi

stop_process_selected() {
  [ "$STOP_ALL" = true ] && return 0
  local module
  module=$(addp_dev_process_module "$1")
  [[ " ${STOP_MODULES[*]} " == *" $module "* ]]
}

if [ "$STOP_ALL" = true ]; then
  echo "🛑 停止 ADDP 开发环境"
else
  echo "🛑 局部停止 ADDP 模块: ${STOP_MODULES[*]}"
fi

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

    if [ "$STOP_ALL" = false ]; then
      local selected=false process_name module
      # launchd 的 ProgramArguments 保留正式产物路径；不能按作业 label 猜模块。
      for process_name in $(printf '%s\n' "$job_info" | sed -n 's|.*\.dev-bins/addp-\([a-z0-9-]*\).*|\1|p'); do
        stop_process_selected "$process_name" && selected=true
      done
      for module in "${STOP_MODULES[@]}"; do
        case "$job_info" in
          *"$ROOT_DIR/$module/frontend"$'\n'*|*"$ROOT_DIR/$module/frontend "*|*"$ROOT_DIR/$module/frontend\""*|*"$ROOT_DIR/$module/frontend") selected=true ;;
        esac
      done
      [ "$selected" = true ] || continue
    fi

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
  local pid="$1" known proc_cmd pidfile owner
  [[ "$pid" =~ ^[0-9]+$ ]] || return 0
  for known in "${all_pids[@]}" "${system_pids[@]}"; do
    [ "$pid" != "$known" ] || return 0
  done
  proc_cmd=$(ps -p "$pid" -o command= 2>/dev/null) || return 0
  if ! addp_dev_pid_owned_by_workspace "$pid"; then
    echo -e "${YELLOW}  ⚠️  跳过其他工作区进程 (PID: $pid)${NC}"
    return 0
  fi
  if [ "$STOP_ALL" = false ]; then
    # PID 可能已被复用，不能因为所选模块的陈旧 PID 文件而停止其他受管模块。
    for pidfile in .dev-pids/*.pid; do
      [ -f "$pidfile" ] || continue
      stop_process_selected "$(basename "$pidfile" .pid)" && continue
      owner=$(cat "$pidfile" 2>/dev/null)
      [[ "$owner" =~ ^[0-9]+$ ]] || continue
      if [ "$pid" = "$owner" ] || addp_process_is_descendant_of "$pid" "$owner"; then
        echo -e "${YELLOW}  ⚠️  跳过未选择模块的进程 (PID: $pid)${NC}"
        return 0
      fi
    done
  fi
  case "$proc_cmd" in
    "$ROOT_DIR/.dev-bins/addp-system"|"$ROOT_DIR/.dev-bins/addp-system "*|.dev-bins/addp-system|.dev-bins/addp-system\ *|./.dev-bins/addp-system|./.dev-bins/addp-system\ *) system_pids+=("$pid") ;;
    *) all_pids+=("$pid") ;;
  esac
  echo "  发现工作区进程 (PID: $pid): ${proc_cmd:0:100}"
}

collect_workspace_listeners() {
  local ports='' name variable preferred saved listener_pids pid raw status=0
  while read -r name variable preferred; do
    stop_process_selected "$name" || continue
    saved=$(addp_dev_saved_port "$variable")
    ports="${ports:+${ports},}${saved:-${!variable:-$preferred}}"
  done < <(addp_dev_port_specs)
  [ -n "$ports" ] || return 0
  raw=$(lsof -nP -a -iTCP:"$ports" -sTCP:LISTEN -Fp 2>&1) || status=$?
  if [ "$status" -ne 0 ] && { [ "$status" -ne 1 ] || [ -n "$raw" ]; }; then
    echo "❌ 无法查询所选模块的监听端口: $raw" >&2
    return 1
  fi
  listener_pids=$(printf '%s\n' "$raw" | sed -n 's/^p\([0-9][0-9]*\)$/\1/p' | sort -u)
  for pid in $listener_pids; do
    add_workspace_stop_pid "$pid"
  done
}

collect_selected_descendants() {
  [ "$STOP_ALL" = false ] || return 0
  local snapshot parent pid ppid pidfile index=0
  snapshot=$(ps -axo pid=,ppid=) || return 1
  # 前端 PID 是 npm 启动器；其监听进程和其他子进程均属于所选生命周期。
  local parents=()
  for pidfile in .dev-pids/*-frontend.pid; do
    [ -f "$pidfile" ] || continue
    stop_process_selected "$(basename "$pidfile" .pid)" || continue
    parent=$(cat "$pidfile" 2>/dev/null)
    [[ " ${all_pids[*]} " == *" $parent "* ]] || continue
    parents+=("$parent")
  done
  while [ "$index" -lt "${#parents[@]}" ]; do
    parent="${parents[$index]}"
    index=$((index + 1))
    while read -r pid ppid; do
      [ "$ppid" = "$parent" ] || continue
      [[ " ${parents[*]} " != *" $pid "* ]] || continue
      parents+=("$pid")
      add_workspace_stop_pid "$pid"
    done <<< "$snapshot"
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
      stop_process_selected "$(basename "$pidfile" .pid)" || continue
      pid=$(cat "$pidfile" 2>/dev/null)
      add_workspace_stop_pid "$pid"
    done
  fi
  echo -e "${YELLOW}检查工作区残留监听者...${NC}"
  collect_workspace_listeners || return 1
  collect_selected_descendants || return 1

  # 保留 System 的 HTTP 和鉴权能力，供其他实例完成注销。
  stop_workspace_launchd_jobs modules || cleanup_failed=true
  stop_pid_group "模块、Worker 和前端" "${all_pids[@]}"

  local name container labels
  for name in document-workflow supermap-workflow; do
    stop_process_selected "$name" || continue
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

while read -r name variable preferred; do
  case "$name" in *-frontend) ;; *) continue ;; esac
  stop_process_selected "$name" || continue
  frontend_dir="$ROOT_DIR/$(addp_dev_process_module "$name")/frontend"
  if [ -d "$frontend_dir" ]; then
    rm -rf "$frontend_dir/node_modules/.vite" "$frontend_dir/.vite" 2>/dev/null || true
  fi
done < <(addp_dev_port_specs)
echo "✓ 前端缓存已清理"

# 保留开发二进制文件（加速重启）
# 如需清理二进制，请手动删除: rm -rf .dev-bins

# 清理 PID 文件
if [ "$STOP_STATUS" -eq 0 ] && [ -d ".dev-pids" ]; then
  for pidfile in .dev-pids/*.pid; do
    [ -f "$pidfile" ] || continue
    stop_process_selected "$(basename "$pidfile" .pid)" || continue
    rm -f "$pidfile"
  done
  rmdir .dev-pids 2>/dev/null || true
  echo "✓ 所选 PID 文件已清理"
fi

echo ""
if [ "$STOP_STATUS" -ne 0 ]; then
  echo -e "${RED}========================================${NC}"
  echo -e "${RED}✗ 服务停止未完成，请先处理上述错误${NC}"
  echo -e "${RED}========================================${NC}"
  exit "$STOP_STATUS"
fi

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ 所选服务已停止并清理完成${NC}"
echo -e "${GREEN}========================================${NC}"
