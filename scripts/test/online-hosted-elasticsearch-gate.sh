#!/usr/bin/env bash
# Elasticsearch catalog, scan, document preview and DSL Console acceptance on GitHub Hosted.
# ADDP_ONLINE_SUITES=elasticsearch-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=elasticsearch-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-elasticsearch-online-disposable)
stop_online_fixture() {
  run_logged bash business/scripts/online-elasticsearch-consumer-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/elasticsearch-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-elasticsearch-consumer-fixture.sh start
application_owned=1
for start_target in -meta -manager -develop; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite elasticsearch-consumer-flow --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_ELASTICSEARCH_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
