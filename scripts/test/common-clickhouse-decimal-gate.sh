#!/usr/bin/env bash
# common-clickhouse-decimal-gate.sh - Verify ClickHouse decimal table-write precision against a disposable database.
# ADDP_T2_HOSTED_ONLY=clickhouse

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CONTAINER_NAME=addp-clickhouse-decimal-disposable
CLICKHOUSE_IMAGE='clickhouse/clickhouse-server:25.12.1.649@sha256:7234d193fbeb3a375f21f0bb99040396f68bb2e82cada4f4947d7fcf9638bbf3'
DATABASE_NAME=addp_clickhouse_disposable
DATABASE_PASSWORD='addp_clickhouse_password'
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-clickhouse-decimal.XXXXXX")
CONTAINER_OWNED=false

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "$WORK_DIR/clickhouse-container.log" 2>&1
            tail -n 200 "$WORK_DIR/clickhouse-container.log" >&2
        fi
        docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-common-clickhouse-decimal.*) rm -rf "$WORK_DIR" ;;
    esac
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

for command in docker go; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "$command is required for the disposable ClickHouse decimal gate" >&2
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
    --publish 127.0.0.1::9000 \
    --env "CLICKHOUSE_DB=$DATABASE_NAME" \
    --env CLICKHOUSE_USER=default \
    --env "CLICKHOUSE_PASSWORD=$DATABASE_PASSWORD" \
    --env CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
    "$CLICKHOUSE_IMAGE" >/dev/null

ready=false
for _ in $(seq 1 60); do
    if docker exec "$CONTAINER_NAME" clickhouse-client --user default --password "$DATABASE_PASSWORD" --query 'SELECT 1' 2>/dev/null | grep -Fxq '1'; then
        ready=true
        break
    fi
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "ClickHouse disposable container exited before readiness" >&2
        exit 1
    fi
    sleep 5
done
if [ "$ready" != true ]; then
    echo "ClickHouse disposable container did not become ready within 300 seconds" >&2
    exit 1
fi

host_mapping=$(docker port "$CONTAINER_NAME" 9000/tcp)
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*)
        echo "cannot determine disposable ClickHouse host port from: $host_mapping" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/common"
ADDP_CLICKHOUSE_INTEGRATION=1 \
ADDP_TEST_CLICKHOUSE_HOST=127.0.0.1 \
ADDP_TEST_CLICKHOUSE_PORT="$host_port" \
ADDP_TEST_CLICKHOUSE_DATABASE="$DATABASE_NAME" \
ADDP_TEST_CLICKHOUSE_USER=default \
ADDP_TEST_CLICKHOUSE_PASSWORD="$DATABASE_PASSWORD" \
    go test ./engine/plugins/clickhouse \
    -run '^TestIntegrationClickHouseDecimalTableWriteDefinition$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-clickhouse-decimal.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-clickhouse-decimal.log"; then
    echo "Common ClickHouse decimal gate refuses skipped tests" >&2
    exit 1
fi
