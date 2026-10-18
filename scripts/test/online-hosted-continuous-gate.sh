#!/usr/bin/env bash
# Real continuous execution events on a disposable Hosted deployment.
# ADDP_ONLINE_SUITES=transfer-continuous-events
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=transfer-continuous-events
HOSTED_FIXTURE_CONTAINERS=(addp-continuous-pg-online-disposable addp-continuous-kafka-online-disposable)
stop_online_fixture() {
  run_logged bash business/scripts/online-continuous-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export MONITOR_URL=http://127.0.0.1:8100
export CONSOLE_URL=http://127.0.0.1:5170
export SECURITY_URL=http://127.0.0.1:8194
export TRANSFER_CONTINUOUS_LEASE_DURATION=8s TRANSFER_CONTINUOUS_HEARTBEAT_INTERVAL=2s
export TRANSFER_CONTINUOUS_CLAIM_INTERVAL=500ms
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-continuous-fixture.sh start
# Reject invalid private descriptors before spending time on application builds.
run_logged python3 -c 'import importlib,sys; from pathlib import Path; module=importlib.import_module("scripts.test.online-engine-registration"); [module.load_descriptor(Path(sys.argv[1])/f"continuous-{kind}.json") for kind in ("pg","kafka")]' "$ADDP_ONLINE_SECRET_DIR"
application_owned=1
for start_target in -security -transfer -monitor; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite transfer-continuous-events --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
for kind in pg kafka; do
  result="$ADDP_ONLINE_SECRET_DIR/continuous-$kind.env"
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/continuous-$kind.json" --output "$result"
  source "$result"
  kind_upper=$(printf '%s' "$kind" | tr '[:lower:]' '[:upper:]')
  printf -v "ADDP_ONLINE_CONTINUOUS_${kind_upper}_ENGINE_ID" '%s' "$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  export "ADDP_ONLINE_CONTINUOUS_${kind_upper}_ENGINE_ID"
done
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
run_logged bash business/scripts/online-continuous-fixture.sh verify-clean
