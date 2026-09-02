#!/usr/bin/env bash
# transfer-postgres-gate.sh - Run Transfer PostgreSQL protected export integration tests.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-transfer-postgres.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

database=${ADDP_TEST_POSTGRES_DATABASE:-addp_test}
case "$database" in
    *test*|*disposable*) ;;
    *)
        echo "ADDP_TEST_POSTGRES_DATABASE must identify a disposable test database" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/transfer/backend"
ADDP_POSTGRES_INTEGRATION=1 \
    ADDP_TEST_POSTGRES_DATABASE="$database" \
    go test ./internal/protection \
    -run '^TestIntegrationPostgresBoundedExportMasksBeforeTargetWrite$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/transfer.log"; then
    echo "Transfer PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
