#!/usr/bin/env bash
# Disposable metric revision lifecycle through GitHub Hosted Ubuntu.
# ADDP_ONLINE_SUITES=metric-service-revision-lifecycle
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=metric-service-revision-lifecycle
HOSTED_FIXTURE_CONTAINERS=(addp-metric-online-disposable)
HOSTED_FIXTURE_IMAGES=()
stop_online_fixture() {
  run_logged bash business/scripts/online-metric-postgres-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export STANDARD_URL=http://127.0.0.1:8110 MODEL_URL=http://127.0.0.1:8181
export SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/metric-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-metric-postgres-fixture.sh start
application_owned=1
for start_target in -model -service -security; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite metric-service-revision-lifecycle --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
