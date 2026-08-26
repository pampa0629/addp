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
[ "$#" -eq 1 ] || fail "usage: bash scripts/dev/stop-exact-process.sh -system|-manager|-gateway"

case "$1" in
  -system)
    module=system
    expected_command='\.dev-bins/addp-system([[:space:]]|$)'
    ;;
  -manager)
    module=manager
    expected_command='\.dev-bins/addp-manager([[:space:]]|$)'
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

kill -TERM "$pid"
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
