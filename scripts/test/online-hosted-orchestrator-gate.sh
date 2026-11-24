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
ORCHESTRATOR_CASE=${ADDP_ONLINE_ORCHESTRATOR_CASE:-execution}
case "$ORCHESTRATOR_CASE" in
  execution) [ -z "${ADDP_ONLINE_ORCHESTRATOR_SOURCE_COMMIT:-}" ] || exit 1 ;;
  scheduled-controlled-file)
    [[ "${ADDP_ONLINE_ORCHESTRATOR_SOURCE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]] || exit 1
    [ "$(git -C "$ROOT_DIR" rev-parse HEAD)" = "$ADDP_ONLINE_ORCHESTRATOR_SOURCE_COMMIT" ] || exit 1
    RUNTIME_CONTAINER=addp-orchestrator-scheduled-runtime
    RUNTIME_TAG=orchestrator-scheduled-file
    RUNTIME_IMAGE=localhost:5001/addp-geopython-workflow-engine:$RUNTIME_TAG
    HOSTED_FIXTURE_CONTAINERS=(addp-orchestrator-scheduled-source addp-orchestrator-scheduled-registry "$RUNTIME_CONTAINER")
    HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim)
    ;;
  *) echo 'Invalid Orchestrator Online case' >&2; exit 1 ;;
esac
registry_owned=0
proxy_pid=
issuer_pid=
alias_owned=0
stop_online_fixture() {
  local status=0 image
  if [ "$ORCHESTRATOR_CASE" = scheduled-controlled-file ]; then
    if [ -n "$issuer_pid" ]; then
      local observer_status=0
      kill -TERM "$issuer_pid" 2>/dev/null || status=1
      wait "$issuer_pid" || observer_status=$?
      [ "$observer_status" -eq 143 ] || [ "$observer_status" -eq 0 ] || status=1
    fi
    run_logged python3 -m scripts.test.orchestrator-scheduled-file-online stop || status=1
    if [ "$registry_owned" -eq 1 ]; then
      remove_online_product_container addp-orchestrator-scheduled-registry || status=1
    fi
    for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
      if docker image inspect "$image" >/dev/null 2>&1; then
        run_logged docker image rm "$image" || status=1
        if docker image inspect "$image" >/dev/null 2>&1; then status=1; fi
      fi
    done
    return "$status"
  fi
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
export ADDP_ONLINE_ORCHESTRATOR_CASE="$ORCHESTRATOR_CASE"
if [ "$ORCHESTRATOR_CASE" = scheduled-controlled-file ]; then
  export ADDP_ONLINE_TEST_RUN_ID="orchestrator-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
  export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=1740
  export SECURITY_URL=http://127.0.0.1:8194
  infra_owned=1
  hosted_start_platform_infra
  ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID=$(docker inspect --format '{{.Id}}' addp-postgres)
  export ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID
  fixture_owned=1
  run_logged python3 -m scripts.test.orchestrator-scheduled-file-online start
  registry_owned=1
  run_logged docker run -d --name addp-orchestrator-scheduled-registry \
    --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
  for attempt in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
    [ "$attempt" -lt 30 ] || fail 'Orchestrator registry readiness timed out'
    sleep 1
  done
  run_logged docker pull python:3.11-slim
  run_logged docker tag python:3.11-slim localhost:5001/python:3.11-slim
  run_logged docker push localhost:5001/python:3.11-slim
  application_owned=1
  hosted_prepare_native_database system meta gateway security
  unset hosted_deploy_password
  for start_target in -security -meta -orchestrator -monitor; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
  done
  python3 -m scripts.test.orchestrator-scheduled-file-online observe-issuance >> "$GATE_LOG" 2>&1 &
  issuer_pid=$!
  for _ in {1..100}; do
    kill -0 "$issuer_pid" 2>/dev/null || fail 'System issuance observer exited before readiness'
    [ ! -s "$ADDP_ONLINE_SECRET_DIR/scheduled-system-port" ] || break
    sleep 0.1
  done
  [ -s "$ADDP_ONLINE_SECRET_DIR/scheduled-system-port" ] || fail 'System issuance observer readiness timed out'
  issuer_port=$(cat "$ADDP_ONLINE_SECRET_DIR/scheduled-system-port")
  [[ "$issuer_port" =~ ^[1-9][0-9]{0,4}$ ]] && [ "$issuer_port" -le 65535 ] || fail 'invalid System issuance observer port'
  run_daemon_launcher_logged env ADDP_ONLINE_DEVELOP_SYSTEM_OBSERVER="http://127.0.0.1:$issuer_port" SKIP_MODTIDY=1 bash scripts/dev/start.sh -develop
  start_online_geopython_runtime "$RUNTIME_TAG"
  run_logged python3 -m scripts.test.orchestrator-scheduled-file-online verify-build
  run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite orchestrator-execution --wait-protection-owners develop --output "$1"' _ "$IDENTITY_ENV"
  source "$IDENTITY_ENV"
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
  export ADDP_ONLINE_CONSUMER_ENGINE_ID
  run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
  exit 0
fi
infra_owned=1
hosted_start_platform_infra
ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID=$(docker inspect --format '{{.Id}}' addp-postgres)
export ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID
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
hosted_prepare_native_database system meta gateway
unset hosted_deploy_password
for start_target in -meta -orchestrator -monitor; do
  case "$start_target" in
    -meta)
      run_daemon_launcher_logged env SERVICE_HOST=addp-orchestrator-meta.test SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
      ;;
    -orchestrator|-monitor)
      run_daemon_launcher_logged env HTTP_PROXY="$ADDP_ONLINE_ORCHESTRATOR_PROXY_URL" NO_PROXY=127.0.0.1,localhost SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
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
