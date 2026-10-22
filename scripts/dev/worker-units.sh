#!/usr/bin/env bash
# Native Worker launch units; source only, no service or environment mutation.
addp_meta_worker_units() {
  case "${ADDP_DEV_META_WORKER_REPLICAS-1}" in
    1) printf '%s\n' meta-worker-1 ;;
    2) printf '%s\n' meta-worker-1 meta-worker-2 ;;
    *) echo 'ADDP_DEV_META_WORKER_REPLICAS must be exactly 1 or 2' >&2; return 1 ;;
  esac
}

addp_worker_binary() {
  case "$1" in
    meta-worker-[12]) printf '%s\n' meta-worker ;;
    *) printf '%s\n' "$1" ;;
  esac
}

addp_meta_worker_pid_owned() {
  local pid="$1" root="${PROJECT_ROOT:-$ROOT_DIR}" cwd command_line
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  command_line=$(ps -p "$pid" -o command= 2>/dev/null) || return 1
  if [ -d "/proc/$pid" ]; then
    cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null) || return 1
  else
    cwd=$(lsof -nP -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)
  fi
  [ "$cwd" = "$root" ] || return 1
  case " $command_line " in
    *" $root/.dev-bins/addp-meta-worker "*|*" .dev-bins/addp-meta-worker "*|*" ./.dev-bins/addp-meta-worker "*) return 0 ;;
  esac
  return 1
}

addp_check_meta_worker_inventory() {
  local path unit pid selected seen=' '
  selected=$(addp_meta_worker_units) || return 1
  selected=" ${selected//$'\n'/ } "
  for path in .dev-pids/meta-worker*.pid; do
    [ -f "$path" ] || continue
    pid=$(cat "$path") || return 1
    [[ "$pid" =~ ^[1-9][0-9]*$ ]] || { echo 'Invalid Meta Worker PID record' >&2; return 1; }
    kill -0 "$pid" 2>/dev/null || continue
    addp_meta_worker_pid_owned "$pid" || { echo 'Meta Worker PID belongs to another process or workspace' >&2; return 1; }
    unit=$(basename "$path" .pid)
    case "$selected" in *" $unit "*) ;; *) echo 'Running Meta Worker is outside the selected units; stop or restart Meta explicitly' >&2; return 1 ;; esac
    case "$seen" in *" $pid "*) echo 'Meta Worker units reference the same running PID' >&2; return 1 ;; esac
    seen="$seen$pid "
  done
}
