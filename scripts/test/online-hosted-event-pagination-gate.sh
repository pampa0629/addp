#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=execution-events-pagination
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
# Real Console/Monitor/Owner reads; Common-owned synthetic execution fixture.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=execution-events-pagination
HOSTED_FIXTURE_CONTAINERS=()
stop_online_fixture() { return 0; }
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170 MONITOR_URL=http://127.0.0.1:8100
infra_owned=1
run_logged bash scripts/infra/up.sh
export ADDP_ONLINE_RETENTION_POSTGRES_ID
ADDP_ONLINE_RETENTION_POSTGRES_ID=$(docker inspect --format '{{.Id}}' addp-postgres)
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh -meta -monitor -console
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite execution-events-pagination --output "$1"' _ "$IDENTITY_ENV"
# Consumer credentials only enter fixture and test subprocesses, never runtimes.
(source "$IDENTITY_ENV"; run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE")
