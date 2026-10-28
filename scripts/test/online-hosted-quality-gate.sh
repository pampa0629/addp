#!/usr/bin/env bash
# Real Model -> Quality execution and safe Monitor events on a disposable VM.
# ADDP_ONLINE_SUITES=quality-dynamic-binding
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=quality-dynamic-binding
quality_case=${ADDP_ONLINE_QUALITY_CASE:-full}
case "$quality_case" in
  full|bounded-recovery) ;;
  *) echo 'Unknown Quality acceptance case' >&2; exit 1 ;;
esac
before_online_application_stop() {
  [ "${ADDP_ONLINE_QUALITY_CASE:-full}" = bounded-recovery ] || return 0
  run_logged python3 -m scripts.test.quality-bounded-recovery-faults resume
}
HOSTED_FIXTURE_CONTAINERS=(addp-quality-online-disposable)
HOSTED_FIXTURE_IMAGES=()
stop_online_fixture() {
  run_logged python3 business/scripts/online-quality-postgres-fixture.py stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_ONLINE_QUALITY_CASE="$quality_case"
if [ "$quality_case" = bounded-recovery ]; then
  export ADDP_DEV_QUALITY_WORKER_REPLICAS=2 QUALITY_WORKER_CONCURRENCY=1
  export QUALITY_WORKER_LEASE_DURATION=30s QUALITY_WORKER_POLL_INTERVAL=500ms
fi
export MODEL_URL=http://127.0.0.1:8181 QUALITY_URL=http://127.0.0.1:8182
export ORCHESTRATOR_URL=http://127.0.0.1:8084 MONITOR_URL=http://127.0.0.1:8100
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/quality-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged python3 business/scripts/online-quality-postgres-fixture.py start
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -model -meta -quality -orchestrator -monitor
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite quality-dynamic-binding --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged python3 -m scripts.test.quality-online-fixture prepare
run_logged python3 business/scripts/online-quality-postgres-fixture.py seed
run_logged python3 -m scripts.test.quality-online-fixture scan
source "$ADDP_ONLINE_SECRET_DIR/quality-fixtures.env"
unset ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
run_logged python3 business/scripts/online-quality-postgres-fixture.py verify
