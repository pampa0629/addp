#!/usr/bin/env bash
# ADDP_T2_SERVICES=mysql
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_MYSQL_PASSWORD|ADDP_LOCAL_CI_MYSQL
# Model compiler semantics through the MySQL Provider, in an automatically removed database.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-model-mysql.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT
if [ "${ADDP_LOCAL_CI_MYSQL:-0}" = "1" ]; then
  export ADDP_TEST_MYSQL_HOST=127.0.0.1 ADDP_TEST_MYSQL_PORT=13306
  export ADDP_TEST_MYSQL_USER=root ADDP_TEST_MYSQL_PASSWORD=addp_local_ci_mysql_password
fi
: "${ADDP_TEST_MYSQL_PASSWORD:?ADDP_TEST_MYSQL_PASSWORD must identify a disposable MySQL 8 instance}"
cd "$ROOT_DIR/model/backend"
ADDP_MYSQL_INTEGRATION=1 go test ./internal/service -run '^TestIntegrationMySQLMetric' -count=1 -v 2>&1 | tee "$WORK_DIR/result.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/result.log"; then
  echo 'Model MySQL gate refuses skipped tests' >&2
  exit 1
fi
