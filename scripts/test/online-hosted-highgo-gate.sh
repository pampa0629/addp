#!/usr/bin/env bash
# HighGo native catalog and PG snapshot replace on disposable GitHub Hosted Linux.
# ADDP_ONLINE_SUITES=highgo-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=highgo-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-highgo-online-disposable addp-highgo-pg-online-disposable)
stop_online_fixture() {
  run_logged python3 business/scripts/online-highgo-consumer-fixture.py stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged python3 business/scripts/online-highgo-consumer-fixture.py start
application_owned=1
for start_target in -transfer -manager; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite highgo-consumer-flow --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/highgo-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_HIGHGO_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/highgo-pg-engine.json" --output "$ADDP_ONLINE_SECRET_DIR/highgo-pg-engine.env"
source "$ADDP_ONLINE_SECRET_DIR/highgo-pg-engine.env"
export ADDP_ONLINE_HIGHGO_PG_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
run_logged python3 business/scripts/online-highgo-consumer-fixture.py verify
