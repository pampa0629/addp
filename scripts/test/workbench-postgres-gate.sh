#!/usr/bin/env bash
# workbench-postgres-gate.sh - Run Workbench owner and optimistic concurrency tests against addp_test.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-workbench-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

if [ -z "${WORKBENCH_POSTGRES_TEST_DSN:-}" ]; then
    echo "WORKBENCH_POSTGRES_TEST_DSN must reference addp_test or an isolated disposable PostgreSQL database" >&2
    exit 1
fi
dsn_without_query=${WORKBENCH_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$WORKBENCH_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "WORKBENCH_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
case "$database" in addp_test|*disposable*) ;; *) echo "WORKBENCH_POSTGRES_TEST_DSN must use addp_test or an isolated disposable database" >&2; exit 1 ;; esac

cd "$ROOT_DIR/workbench/backend"
go test ./internal/repository -run '^TestViewRepositoryAgainstPostgres$' -count=1 -v 2>&1 | tee "$WORK_DIR/workbench.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/workbench.log"; then
    echo "Workbench PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
