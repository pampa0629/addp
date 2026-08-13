#!/bin/bash
# lifecycle-lock.sh - ADDP 开发服务工作区生命周期互斥锁

addp_process_is_descendant_of() {
  local process_pid="$1"
  local ancestor_pid="$2"
  local current_pid="$process_pid"

  while [ -n "$current_pid" ] && [ "$current_pid" -gt 1 ] 2>/dev/null; do
    if [ "$current_pid" = "$ancestor_pid" ]; then
      return 0
    fi
    current_pid=$(ps -p "$current_pid" -o ppid= 2>/dev/null | tr -d ' ' || true)
  done
  return 1
}

addp_read_lifecycle_lock_field() {
  local field="$1"
  local metadata_file="$2"
  sed -n "s/^${field}=//p" "$metadata_file" 2>/dev/null | head -n 1
}

addp_acquire_lifecycle_lock() {
  local operation="$1"
  shift || true

  ADDP_LIFECYCLE_STATE_DIR="${ROOT_DIR}/.dev-state"
  ADDP_LIFECYCLE_LOCK_DIR="${ADDP_LIFECYCLE_STATE_DIR}/lifecycle.lock"
  ADDP_LIFECYCLE_LOCK_METADATA="${ADDP_LIFECYCLE_LOCK_DIR}/owner"
  mkdir -p "$ADDP_LIFECYCLE_STATE_DIR"

  if [ -n "${ADDP_LIFECYCLE_OWNER_PID:-}" ] && [ -d "$ADDP_LIFECYCLE_LOCK_DIR" ]; then
    local recorded_owner
    recorded_owner=$(addp_read_lifecycle_lock_field pid "$ADDP_LIFECYCLE_LOCK_METADATA")
    if [ "$recorded_owner" = "$ADDP_LIFECYCLE_OWNER_PID" ] &&
      addp_process_is_descendant_of "$$" "$ADDP_LIFECYCLE_OWNER_PID"; then
      if [ "$$" = "$ADDP_LIFECYCLE_OWNER_PID" ]; then
        ADDP_LIFECYCLE_LOCK_INHERITED=0
        trap addp_release_lifecycle_lock EXIT
        trap 'exit 130' INT
        trap 'exit 143' TERM
      else
        ADDP_LIFECYCLE_LOCK_INHERITED=1
      fi
      export ADDP_LIFECYCLE_OWNER_PID ADDP_LIFECYCLE_LOCK_DIR ADDP_LIFECYCLE_LOCK_INHERITED
      return 0
    fi
    echo "❌ 无效的开发环境生命周期锁继承，已拒绝执行 ${operation}" >&2
    return 1
  fi

  if ! mkdir "$ADDP_LIFECYCLE_LOCK_DIR" 2>/dev/null; then
    local holder_pid holder_operation holder_args holder_started
    holder_pid=$(addp_read_lifecycle_lock_field pid "$ADDP_LIFECYCLE_LOCK_METADATA")
    holder_operation=$(addp_read_lifecycle_lock_field operation "$ADDP_LIFECYCLE_LOCK_METADATA")
    holder_args=$(addp_read_lifecycle_lock_field args "$ADDP_LIFECYCLE_LOCK_METADATA")
    holder_started=$(addp_read_lifecycle_lock_field started_at "$ADDP_LIFECYCLE_LOCK_METADATA")

    if [ -n "$holder_pid" ] && ! ps -p "$holder_pid" >/dev/null 2>&1; then
      echo "❌ 检测到失效的开发环境生命周期锁：${ADDP_LIFECYCLE_LOCK_DIR}" >&2
      echo "   原持有进程 PID ${holder_pid} 已不存在，请确认没有生命周期操作后删除该锁目录。" >&2
      return 1
    fi

    echo "❌ ADDP 开发环境正在执行生命周期操作，拒绝并行执行 ${operation}" >&2
    echo "   当前操作: ${holder_operation:-unknown} ${holder_args:-}" >&2
    echo "   持有进程: ${holder_pid:-unknown}" >&2
    echo "   开始时间: ${holder_started:-unknown}" >&2
    return 1
  fi

  ADDP_LIFECYCLE_OWNER_PID="$$"
  ADDP_LIFECYCLE_LOCK_INHERITED=0
  export ADDP_LIFECYCLE_OWNER_PID ADDP_LIFECYCLE_LOCK_DIR ADDP_LIFECYCLE_LOCK_INHERITED
  {
    printf 'pid=%s\n' "$$"
    printf 'operation=%s\n' "$operation"
    printf 'args=%s\n' "$*"
    printf 'started_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'workspace=%s\n' "$ROOT_DIR"
  } > "$ADDP_LIFECYCLE_LOCK_METADATA"

  trap addp_release_lifecycle_lock EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

addp_release_lifecycle_lock() {
  local exit_code=$?
  trap - EXIT INT TERM
  if [ "${ADDP_LIFECYCLE_LOCK_INHERITED:-1}" = "0" ] &&
    [ "${ADDP_LIFECYCLE_OWNER_PID:-}" = "$$" ] &&
    [ -d "${ADDP_LIFECYCLE_LOCK_DIR:-}" ]; then
    rm -rf "$ADDP_LIFECYCLE_LOCK_DIR"
  fi
  return "$exit_code"
}
