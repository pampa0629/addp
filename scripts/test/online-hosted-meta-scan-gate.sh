#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=meta-scan-claim
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
HARNESS=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ROOT_DIR="${RUNNER_TEMP:?}/meta-scan-product"
[[ "${META_SCAN_SOURCE_COMMIT:-}" =~ ^[a-f0-9]{40}$ ]]
[[ "$(git -C "$ROOT_DIR" rev-parse HEAD)" == "$META_SCAN_SOURCE_COMMIT" ]]
[[ "$(git -C "$HARNESS" rev-parse HEAD)" == "${GITHUB_SHA:?}" ]]
ONLINE_SUITE=meta-scan-claim
HOSTED_FIXTURE_IMAGES=()
HOSTED_FIXTURE_CONTAINERS=(addp-metric-online-disposable)
stop_online_fixture() {
  run_logged bash business/scripts/online-metric-postgres-fixture.sh stop || return 1
  ! docker container inspect addp-metric-online-disposable >/dev/null 2>&1
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"

export ADDP_INFRA_DATABASE_PROVIDER=postgres
export META_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-metric-postgres-fixture.sh start
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
# The product launcher waits for Meta Backend readiness before launching its workers.
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -meta
# Reuse the selected product's System-owned PG source operator and registration user.
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite pg-source-authorization --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make -C "$HARNESS" test-online "ONLINE_SUITE=$ONLINE_SUITE"
