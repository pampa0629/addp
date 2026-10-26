#!/usr/bin/env bash
# Real System maintenance, synthetic Common history; no Owner result mutation.
# ADDP_ONLINE_SUITES=execution-retention-readonly
# ADDP_ONLINE_SUITES=execution-events-retention
# ADDP_ONLINE_SUITES=execution-retention-hourly
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=${ONLINE_SUITE_INPUT:-execution-retention-readonly}
case "$ONLINE_SUITE" in
  execution-retention-readonly) RETENTION_DRIVER=scripts.test.execution-retention-readonly-online ;;
  execution-events-retention) RETENTION_DRIVER=scripts.test.execution-events-retention-online ;;
  execution-retention-hourly) RETENTION_DRIVER=scripts.test.execution-retention-hourly-online ;;
  *) echo 'Unsupported execution maintenance suite' >&2; exit 1 ;;
esac
HOSTED_FIXTURE_CONTAINERS=()
stop_online_fixture() { return 0; }
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
infra_owned=1
run_logged bash scripts/infra/up.sh
export ADDP_ONLINE_RETENTION_POSTGRES_ID
ADDP_ONLINE_RETENTION_POSTGRES_ID=$(docker inspect --format '{{.Id}}' addp-postgres)
application_owned=1
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$2" --output "$1"' _ "$IDENTITY_ENV" "$ONLINE_SUITE"
# Test User credentials stay in acceptance subprocesses, never product runtimes.
(source "$IDENTITY_ENV"; run_logged python3 -m "$RETENTION_DRIVER" authorize)
run_logged bash scripts/dev/stop-exact-process.sh -system
(source "$IDENTITY_ENV"; run_logged python3 -m "$RETENTION_DRIVER" prepare)
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -gateway
# Only the bounded observer gets the long test budget; never a product runtime.
(source "$IDENTITY_ENV"
 if [ "$ONLINE_SUITE" = execution-retention-hourly ]; then export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=8100; fi
 run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE")
# Common history is deliberately retained until this run's Infra destruction.
# hosted-online.sh proves application/Infra/credential cleanup on every exit.
