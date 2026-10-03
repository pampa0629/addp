#!/usr/bin/env bash
# Real nested execution and tenant diagnostics on a disposable Hosted deployment.
# ADDP_ONLINE_SUITES=orchestrator-execution
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=orchestrator-execution
HOSTED_FIXTURE_CONTAINERS=(addp-metric-online-disposable)
HOSTED_FIXTURE_IMAGES=()
proxy_pid=
alias_owned=0
stop_online_fixture() {
  local status=0
  if [ -n "$proxy_pid" ]; then
    kill -TERM "$proxy_pid" 2>/dev/null || status=1
    wait "$proxy_pid" || status=1
  fi
  if [ "$alias_owned" -eq 1 ]; then
    run_logged python3 scripts/test/orchestrator-execution-faults.py alias-remove || status=1
  fi
  run_logged bash business/scripts/online-metric-postgres-fixture.sh stop || status=1
  return "$status"
}
command -v sudo >/dev/null 2>&1 || { echo 'Hosted Orchestrator requires sudo for its owned network alias' >&2; exit 1; }
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ORCHESTRATOR_URL=http://127.0.0.1:8084 MONITOR_URL=http://127.0.0.1:8100
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/orchestrator-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-metric-postgres-fixture.sh start
alias_owned=1
run_logged python3 scripts/test/orchestrator-execution-faults.py alias-add
python3 scripts/test/orchestrator-execution-faults.py proxy --target-port 8082 >> "$GATE_LOG" 2>&1 &
proxy_pid=$!
for _ in {1..100}; do
  kill -0 "$proxy_pid" 2>/dev/null || fail 'fault proxy exited before readiness'
  [ ! -f "$ADDP_ONLINE_SECRET_DIR/orchestrator-proxy.env" ] || break
  sleep 0.1
done
[ -f "$ADDP_ONLINE_SECRET_DIR/orchestrator-proxy.env" ] || fail 'fault proxy readiness timed out'
source "$ADDP_ONLINE_SECRET_DIR/orchestrator-proxy.env"
export ADDP_ONLINE_ORCHESTRATOR_PROXY_URL
application_owned=1
for start_target in -meta -orchestrator -monitor; do
  case "$start_target" in
    -meta)
      run_daemon_launcher_logged env SERVICE_HOST=addp-orchestrator-meta.test SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
      ;;
    -orchestrator)
      run_daemon_launcher_logged env HTTP_PROXY="$ADDP_ONLINE_ORCHESTRATOR_PROXY_URL" NO_PROXY=127.0.0.1,localhost SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
      ;;
    -monitor)
      run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
      ;;
  esac
done
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite orchestrator-execution --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
