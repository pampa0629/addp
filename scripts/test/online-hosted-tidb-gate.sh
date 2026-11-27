#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=tidb-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ONLINE_SUITE=tidb-consumer-flow
HOSTED_FIXTURE_COMPOSE_PROJECTS=(addp-online-tidb-consumer)
HOSTED_FIXTURE_CONTAINERS=()
HOSTED_FIXTURE_IMAGES=()

before_online_application_stop() {
  [ ! -f "$ADDP_ONLINE_SECRET_DIR/tidb-grants.json" ] ||
    run_logged bash -c 'source "$1"; python3 -m scripts.test.database-node-read-online cleanup' _ "$IDENTITY_ENV"
}
stop_online_fixture() {
  run_logged bash business/scripts/online-tidb-consumer-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/tidb-engine.json"
infra_owned=1
hosted_start_platform_infra
fixture_owned=1
run_logged bash business/scripts/online-tidb-consumer-fixture.sh start
application_owned=1
hosted_start_native_modules -security -manager -develop -service -transfer
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite tidb-consumer-flow --wait-protection-owners manager,develop,service,transfer --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_CONSUMER_ENGINE_ID
export ADDP_ONLINE_CONSUMER_ENGINE_TYPE=tidb
export ADDP_ONLINE_CONSUMER_NAMESPACE=tidb_fixture
run_logged python3 -m scripts.test.database-node-read-online verify
unset ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
