#!/usr/bin/env bash
# Disposable PostgreSQL SQL ETL and field lineage acceptance on GitHub Hosted.
# ADDP_ONLINE_SUITES=transfer-relational-sql-etl
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=transfer-relational-sql-etl
HOSTED_FIXTURE_CONTAINERS=(addp-transfer-online-disposable addp-transfer-mongodb-online-disposable)
stop_online_fixture() {
  run_logged bash business/scripts/online-transfer-relational-sql-etl-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ORCHESTRATOR_URL=http://127.0.0.1:8084 MONITOR_URL=http://127.0.0.1:8100
export CONSOLE_URL=http://127.0.0.1:5170
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/transfer-engine.json"
export ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/transfer-mongodb-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start
application_owned=1
for start_target in -transfer -manager -develop -orchestrator -monitor; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite transfer-relational-sql-etl --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
export ADDP_ONLINE_TEST_ENGINE_NAME='Hosted Transfer PostgreSQL'
MONGODB_ENGINE_RESULT_ENV="$ADDP_ONLINE_SECRET_DIR/transfer-mongodb-engine.env"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE" --output "$MONGODB_ENGINE_RESULT_ENV"
source "$MONGODB_ENGINE_RESULT_ENV"
export ADDP_ONLINE_TEST_MONGODB_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
export ADDP_ONLINE_TEST_MONGODB_ENGINE_NAME='Hosted Transfer MongoDB'
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
run_logged bash business/scripts/online-transfer-relational-sql-etl-fixture.sh verify
