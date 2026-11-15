#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_POSTGRES_PORT,ADDP_TEST_POSTGRES_PASSWORD
# Monitor's execution visibility, aggregation and existing notification integration.
set -euo pipefail
test_case=all
run_pattern='^TestIntegrationPostgres'
test_packages=(./internal/repository ./internal/service)
if [ "$#" -gt 0 ]; then
  if [ "$#" -ne 1 ] || [ "$1" != '--case=cdc-performance' ]; then
    echo 'usage: monitor-postgres-gate.sh [--case=cdc-performance]' >&2
    exit 2
  fi
  test_case=cdc-performance
  run_pattern='^TestIntegrationPostgresCDCPerformanceRules$'
  test_packages=(./internal/service)
fi
: "${ADDP_TEST_POSTGRES_PORT:?Set the verified PostgreSQL test service mapping}"
: "${ADDP_TEST_POSTGRES_PASSWORD:?Set the PostgreSQL test service password}"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source "$ROOT_DIR/scripts/infra/ports.sh"
database=${ADDP_TEST_POSTGRES_DATABASE:-addp_test}
addp_infra_verify_test_postgres_target "${ADDP_TEST_POSTGRES_HOST:-localhost}" "$ADDP_TEST_POSTGRES_PORT" "$database"
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-monitor-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT
cd "$ROOT_DIR/monitor/backend"
printf 'Monitor PostgreSQL case: %s\n' "$test_case"
GOWORK=off ADDP_POSTGRES_INTEGRATION=1 ADDP_TEST_POSTGRES_DATABASE="$database" \
  go test "${test_packages[@]}" -run "$run_pattern" \
  -count=1 -v 2>&1 | tee "$WORK_DIR/monitor-postgres.log"
if grep -Eq -- '--- SKIP:|\[no tests to run\]' "$WORK_DIR/monitor-postgres.log"; then
  echo 'Monitor PostgreSQL gate refuses missing or skipped integration tests' >&2
  exit 1
fi
