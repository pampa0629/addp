#!/usr/bin/env bash
# manager-mongodb-security-gate.sh - Verify Manager and Transfer protection against MongoDB dynamic documents.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-manager-mongodb-security.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT

cd "$ROOT_DIR/manager/backend"
ADDP_MONGODB_SECURITY_E2E=1 \
    go test ./internal/api \
    -run '^TestIntegrationManagerMongoOutdoorPersonsPreviewMasksPhone$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/manager-mongodb-security.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/manager-mongodb-security.log"; then
    echo "Manager MongoDB Security gate refuses skipped tests" >&2
    exit 1
fi

cd "$ROOT_DIR/common"
ADDP_MONGODB_SCHEMA_E2E=1 \
    go test ./engine/plugins/mongodb \
    -run '^(TestIntegrationPreparedQueryReadSetAndExecutionUseOutdoorPersonsPlan|TestIntegrationEncodedRecordReadSessionExportsOutdoorPersonsCanonicalExtendedJSON)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-mongodb-query-protection.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-mongodb-query-protection.log"; then
    echo "Common MongoDB query protection gate refuses skipped tests" >&2
    exit 1
fi

cd "$ROOT_DIR/transfer/backend"
ADDP_MONGODB_SECURITY_E2E=1 \
    go test ./internal/protection \
    -run '^TestIntegrationMongoEncodedRecordExportMasksOutdoorPersonsBeforeCanonicalExtendedJSON$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer-mongodb-export-protection.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/transfer-mongodb-export-protection.log"; then
    echo "Transfer MongoDB export protection gate refuses skipped tests" >&2
    exit 1
fi

cd "$ROOT_DIR/transfer/backend"
go test ./internal/service \
    -run '^TestEncodedRecordManagerInfraExportCommitsStableOutputAndFinishesSuccess$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/transfer-manager-infra-export-status.log"
