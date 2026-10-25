#!/usr/bin/env bash
# Native Worker launch units; source only, no service or environment mutation.
addp_worker_units() {
  local family="$1" setting count
  case "$family" in
    quality-worker) setting=ADDP_DEV_QUALITY_WORKER_REPLICAS ;;
    meta-worker) setting=ADDP_DEV_META_WORKER_REPLICAS ;;
    transfer-bounded-worker) setting=ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS ;;
    transfer-continuous-worker) setting=ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS ;;
    *) echo 'Unknown Worker family' >&2; return 1 ;;
  esac
  count="${!setting-1}"
  case "$count" in
    1|2) ;;
    *) echo "$setting must be exactly 1 or 2" >&2; return 1 ;;
  esac
  if [ "$family" = transfer-continuous-worker ] && [ "$count" = 2 ] &&
      [ -n "${TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID:-}" ]; then
    echo 'Two continuous Workers require independently generated instance identities; remove TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID' >&2
    return 1
  fi
  printf '%s-1\n' "$family"
  [ "$count" != 2 ] || printf '%s-2\n' "$family"
}

addp_worker_binary() {
  case "$1" in
    quality-worker-[12]|meta-worker-[12]|transfer-bounded-worker-[12]|transfer-continuous-worker-[12]) printf '%s\n' "${1%-*}" ;;
    *) printf '%s\n' "$1" ;;
  esac
}

addp_worker_pid_owned() {
  local pid="$1" binary="$2" root="${PROJECT_ROOT:-$ROOT_DIR}" cwd command_line
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  command_line=$(ps -p "$pid" -o command= 2>/dev/null) || return 1
  if [ -d "/proc/$pid" ]; then
    cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null) || return 1
  else
    cwd=$(lsof -nP -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)
  fi
  [ "$cwd" = "$root" ] || return 1
  case " $command_line " in
    *" $root/.dev-bins/addp-$binary "*|*" .dev-bins/addp-$binary "*|*" ./.dev-bins/addp-$binary "*) return 0 ;;
  esac
  return 1
}

addp_check_worker_inventory() {
  local family="$1" path unit pid selected seen=' '
  selected=$(addp_worker_units "$family") || return 1
  selected=" ${selected//$'\n'/ } "
  for path in .dev-pids/"$family"*.pid; do
    [ -f "$path" ] || continue
    pid=$(cat "$path") || return 1
    [[ "$pid" =~ ^[1-9][0-9]*$ ]] || { echo 'Invalid Worker PID record' >&2; return 1; }
    kill -0 "$pid" 2>/dev/null || continue
    addp_worker_pid_owned "$pid" "$family" || { echo 'Worker PID belongs to another process or workspace' >&2; return 1; }
    unit=$(basename "$path" .pid)
    case "$selected" in *" $unit "*) ;; *) echo 'Running Worker is outside the selected units; stop or restart its module explicitly' >&2; return 1 ;; esac
    case "$seen" in *" $pid "*) echo 'Worker units reference the same running PID' >&2; return 1 ;; esac
    seen="$seen$pid "
  done
}

# Selected launch plan only; no listener/PID/metrics discovery is used as intent.
addp_dev_publish_service_expectations() (
  local name variable preferred flag module family unit units spec
  local selected=()
  while read -r name variable preferred; do
    case "$variable" in
      *_BACKEND_PORT)
        flag="START_${variable%_PORT}"
        if [ "${!flag:-false}" = true ]; then
          module=$(addp_dev_process_module "$name") || return 1
          selected+=("$module-backend")
        fi ;;
    esac
  done < <(addp_dev_port_specs)
  for spec in 'meta-worker:START_META_WORKER' 'quality-worker:START_QUALITY_WORKER' \
              'transfer-bounded-worker:START_TRANSFER_BOUNDED_WORKER' 'transfer-continuous-worker:START_TRANSFER_CONTINUOUS_WORKER'; do
    family="${spec%:*}"; flag="${spec#*:}"
    [ "${!flag:-false}" = true ] || continue
    units=$(addp_worker_units "$family") || return 1
    while IFS= read -r unit; do selected+=("$unit"); done <<< "$units"
  done
  [ "${#selected[@]}" -gt 0 ] || return 0
  python3 "$ROOT_DIR/scripts/utils/deployment-inventory.py" service-expectations --state running --units "${selected[@]}"
)
