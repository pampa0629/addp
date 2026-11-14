#!/usr/bin/env bash
# Stop one managed ADDP process for dedicated Online lifecycle acceptance.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)

fail() {
  echo "Exact process stop failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] ||
  fail "ADDP_ONLINE_HOST must be exactly 1"
operation=stop
force=0
if [ "${1:-}" = --pause ] || [ "${1:-}" = --resume ]; then
  operation=$1; shift
  [ "${1:-}" = -manager ] || fail "pause/resume requires the managed Manager"
fi
if [ "${1:-}" = "--force" ]; then
  force=1
  shift
fi
[ "$#" -eq 1 ] || fail "usage: bash scripts/dev/stop-exact-process.sh [--force] -system|-manager|-gateway|-copilot|-meta-worker-1|-meta-worker-2|-quality-worker-[12]|-transfer-bounded-worker-[12]|-transfer-continuous-worker-[12]"

case "$1" in
  -system)
    module=system
    expected_command='\.dev-bins/addp-system([[:space:]]|$)'
    ;;
  -manager|-catalog)
    module="${1#-}"
    expected_command="\.dev-bins/addp-${module}([[:space:]]|$)"
    ;;
  -meta-worker-1|-meta-worker-2)
    module="${1#-}"
    expected_command='\.dev-bins/addp-meta-worker([[:space:]]|$)'
    ;;
  -quality-worker-[12]|-transfer-bounded-worker-[12]|-transfer-continuous-worker-[12])
    module="${1#-}"
    expected_command="\\.dev-bins/addp-${module%-*}([[:space:]]|$)"
    ;;
  -copilot)
    module=copilot-backend
    expected_command="venv/bin/python([[:space:]]|[0-9.])* main\.py([[:space:]]|$)"
    ;;
  -gateway)
    module=gateway
    expected_command='\.dev-bins/addp-gateway([[:space:]]|$)'
    ;;
  *)
    fail "unsupported process selector: $1"
    ;;
esac

cd "$ROOT_DIR"
source "${SCRIPT_DIR}/lifecycle-lock.sh"
addp_acquire_lifecycle_lock stop-exact-process "$1"

process_running() {
  local target_pid=$1
  local state
  state=$(ps -p "$target_pid" -o stat= 2>/dev/null || true)
  [ -n "$state" ] && [[ "$state" != Z* ]]
}

pidfile=".dev-pids/${module}.pid"
[ -f "$pidfile" ] || fail "managed PID file does not exist: $pidfile"
pid=$(cat "$pidfile" 2>/dev/null || true)
[[ "$pid" =~ ^[1-9][0-9]*$ ]] || fail "managed PID file is invalid: $pidfile"
process_running "$pid" || fail "managed process is not running: $module PID $pid"
command_line=$(ps -p "$pid" -o command= 2>/dev/null || true)
[[ "$command_line" =~ $expected_command ]] ||
  fail "PID $pid is not the managed $module binary"

# Relative binary names alone cannot prove checkout ownership.
if [ -d "/proc/$pid" ]; then
  process_cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null || true)
else
  process_cwd=$(lsof -nP -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)
fi
expected_cwd="$ROOT_DIR"
[ "$module" != copilot-backend ] || expected_cwd="$ROOT_DIR/copilot/backend"
[ "$process_cwd" = "$expected_cwd" ] || fail "PID $pid does not belong to this checkout"

case "$operation" in
  --pause) kill -STOP "$pid"; exit 0 ;;
  --resume) kill -CONT "$pid"; exit 0 ;;
esac

if [ "$force" -eq 1 ]; then
  kill -KILL "$pid"
else
  kill -TERM "$pid"
fi
for _ in {1..50}; do
  if ! process_running "$pid"; then
    rm -f "$pidfile"
    echo "Stopped exact ADDP process: $module"
    exit 0
  fi
  sleep 0.1
done

kill -KILL "$pid"
for _ in {1..20}; do
  if ! process_running "$pid"; then
    rm -f "$pidfile"
    echo "Stopped exact ADDP process after forced termination: $module"
    exit 0
  fi
  sleep 0.1
done

fail "managed process did not stop: $module PID $pid"
