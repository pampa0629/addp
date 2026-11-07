#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=sql-pool-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=sql-pool-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-sql-pool-postgresql addp-sql-pool-mysql)

before_online_application_stop() {
  [ ! -f "$ADDP_ONLINE_SECRET_DIR/pool-grants.json" ] ||
    run_logged bash -c 'source "$1"; python3 -m scripts.test.sql-pool-consumer-flow-online cleanup' _ "$IDENTITY_ENV"
}

stop_online_fixture() {
  run_logged python3 -m scripts.test.sql-pool-consumer-flow-online stop
}

source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170 SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_TEST_RUN_ID="run-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged python3 -m scripts.test.sql-pool-consumer-flow-online start
application_owned=1
for target in -system -security -meta -manager -develop; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite sql-pool-consumer-flow --wait-protection-owners manager,develop --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 -m scripts.test.sql-pool-consumer-flow-online prepare
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
