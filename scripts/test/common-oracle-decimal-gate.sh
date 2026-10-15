#!/usr/bin/env bash
# Verify Oracle catalog/typed writes in a disposable database, or an explicit
# existing instance's catalog with --read-only (no fixture or business writes).
# ADDP_T2_HOSTED_ONLY=oracle

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CONTAINER_NAME=addp-oracle-decimal-disposable
ORACLE_IMAGE='gvenzl/oracle-free:23-slim@sha256:fbbd3023d5abc33e36d3814816e6fd740e8efabeaa70cf470ddeab5874a3f6f8'
SPATIAL_INTEGRATION=0
TEST_PATTERN='^TestIntegrationOracle(DecimalTableWriteDefinition|TypedJSONWrites|PartitionedApplyScalarTypeMatrix|ReadOnlyCatalog)$'
ORACLE_PASSWORD='addp_oracle_sys_password'
ORACLE_USER=addp
ORACLE_APP_PASSWORD='addp_oracle_password'
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-oracle-decimal.XXXXXX")
CONTAINER_OWNED=false

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "$WORK_DIR/oracle-container.log" 2>&1
            tail -n 200 "$WORK_DIR/oracle-container.log" >&2
        fi
        docker rm --force --volumes "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
        if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
            echo "Oracle gate left its owned container behind: $CONTAINER_NAME" >&2
            status=1
        fi
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-common-oracle-decimal.*) rm -rf "$WORK_DIR" ;;
    esac
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "${1:-}" in
    --read-only)
        if [ "$#" -ne 1 ]; then
            echo "read-only Oracle verification accepts no additional arguments" >&2
            exit 2
        fi
        for required in ADDP_TEST_ORACLE_HOST ADDP_TEST_ORACLE_PORT ADDP_TEST_ORACLE_SERVICE_NAME ADDP_TEST_ORACLE_USER ADDP_TEST_ORACLE_PASSWORD; do
            if [ -z "${!required:-}" ]; then
                echo "$required is required for read-only Oracle verification" >&2
                exit 2
            fi
        done
        command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
        cd "$ROOT_DIR/common"
        ADDP_ORACLE_READONLY_INTEGRATION=1 \
            go test ./engine/plugins/oracle -run '^TestIntegrationOracleReadOnlyCatalog$' \
            -count=1 -v 2>&1 | tee "$WORK_DIR/oracle-readonly.log"
        if grep -q -- '--- SKIP:' "$WORK_DIR/oracle-readonly.log"; then
            echo "read-only Oracle verification refuses skipped tests" >&2
            exit 1
        fi
        exit 0
        ;;
    --spatial)
        if [ "$#" -ne 1 ]; then
            echo "spatial Oracle verification accepts no additional arguments" >&2
            exit 2
        fi
        ORACLE_IMAGE='gvenzl/oracle-free:23@sha256:e2763af84ecc345d48e4e3fe7999d0b7bb57885b6e0166051acd5f62e27eb605'
        SPATIAL_INTEGRATION=1
        TEST_PATTERN='^TestIntegrationOracle(DecimalTableWriteDefinition|TypedJSONWrites|PartitionedApplyScalarTypeMatrix|ReadOnlyCatalog|SpatialDelete)$'
        ;;
    '')
        if [ "$#" -ne 0 ]; then
            echo "Oracle verification accepts no empty arguments" >&2
            exit 2
        fi
        ;;
    *) echo "unknown Oracle verification mode: $1" >&2; exit 2 ;;
esac

for command in docker go; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "$command is required for the disposable Oracle decimal gate" >&2
        exit 1
    fi
done
if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    echo "refusing to replace existing container: $CONTAINER_NAME" >&2
    exit 1
fi

CONTAINER_OWNED=true
docker run --detach \
    --name "$CONTAINER_NAME" \
    --publish 127.0.0.1::1521 \
    --env "ORACLE_PASSWORD=$ORACLE_PASSWORD" \
    --env "APP_USER=$ORACLE_USER" \
    --env "APP_USER_PASSWORD=$ORACLE_APP_PASSWORD" \
    "$ORACLE_IMAGE" >/dev/null

ready=false
for _ in $(seq 1 120); do
    if docker exec "$CONTAINER_NAME" healthcheck.sh >/dev/null 2>&1; then
        ready=true
        break
    fi
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "Oracle disposable container exited before readiness" >&2
        exit 1
    fi
    sleep 5
done
if [ "$ready" != true ]; then
    echo "Oracle disposable container did not become ready within 600 seconds" >&2
    exit 1
fi

host_mapping=$(docker port "$CONTAINER_NAME" 1521/tcp)
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*)
        echo "cannot determine disposable Oracle host port from: $host_mapping" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/common"
ADDP_ORACLE_SPATIAL_INTEGRATION="$SPATIAL_INTEGRATION" \
ADDP_ORACLE_DECIMAL_INTEGRATION=1 \
ADDP_ORACLE_JSON_INTEGRATION=1 \
ADDP_ORACLE_INTEGRATION=1 \
ADDP_ORACLE_READONLY_INTEGRATION=1 \
ADDP_TEST_ORACLE_HOST=127.0.0.1 \
ADDP_TEST_ORACLE_PORT="$host_port" \
ADDP_TEST_ORACLE_SERVICE_NAME=FREEPDB1 \
ADDP_TEST_ORACLE_USER="$ORACLE_USER" \
ADDP_TEST_ORACLE_PASSWORD="$ORACLE_APP_PASSWORD" \
    go test ./engine/plugins/oracle \
    -run "$TEST_PATTERN" \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-oracle-decimal.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-oracle-decimal.log"; then
    echo "Common Oracle decimal gate refuses skipped tests" >&2
    exit 1
fi
