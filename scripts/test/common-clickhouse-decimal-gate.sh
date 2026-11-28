#!/usr/bin/env bash
# common-clickhouse-decimal-gate.sh - Verify ClickHouse typed writes, guarded reads and Manager preview on a disposable database.
# ADDP_T2_HOSTED_ONLY=clickhouse

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CONTAINER_NAME=addp-clickhouse-decimal-disposable
CLICKHOUSE_IMAGE='clickhouse/clickhouse-server:25.12.1.649@sha256:7234d193fbeb3a375f21f0bb99040396f68bb2e82cada4f4947d7fcf9638bbf3'
DATABASE_NAME=addp_clickhouse_disposable
DATABASE_PASSWORD='addp_clickhouse_password'
MODE=${1:-test}
OWNER=clickhouse-native-test
case "$MODE" in
    test) ;;
    online-start|online-stop|online-audit)
        [ "${GITHUB_ACTIONS:-}/${RUNNER_OS:-}/${ADDP_ONLINE_HOST:-}/${ONLINE_SUITE_INPUT:-}" = "true/Linux/1/clickhouse-consumer-flow" ] ||
            { echo "disposable Hosted ClickHouse required" >&2; exit 1; }
        CONTAINER_NAME=addp-clickhouse-source-online
        OWNER="clickhouse-consumer-flow:${GITHUB_RUN_ID:?}:${GITHUB_RUN_ATTEMPT:?}"
        DATABASE_NAME=business
        ;;
    *) exit 2 ;;
esac

check_owned() {
    [ "$(docker inspect --format '{{index .Config.Labels "com.addp.online-fixture"}}' "$CONTAINER_NAME")" = "$OWNER" ]
}

if [ "$MODE" = online-stop ]; then
    if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        check_owned
        volumes=$(docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{println .Name}}{{end}}{{end}}' "$CONTAINER_NAME")
        docker rm -fv "$CONTAINER_NAME" >/dev/null
        for volume in $volumes; do
            [ -z "$(docker volume ls -q --filter "name=^$volume$")" ]
        done
    fi
    [ -z "$(docker ps -aq --filter "label=com.addp.online-fixture=$OWNER")" ]
    echo 'ClickHouse Hosted source cleanup passed'
    exit 0
fi

admin_sql() {
    docker exec -i "$CONTAINER_NAME" sh -c 'exec clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" --database "$CLICKHOUSE_DB" --multiquery'
}

if [ "$MODE" = online-audit ]; then
    check_owned
    admin_sql <<'SQL'
SYSTEM FLUSH LOGS;
SELECT query_id
FROM system.query_log
WHERE initial_user = 'business_reader' AND query_kind = 'Select'
AND ((type = 'QueryStart' AND query IN (
    'SELECT optional_text FROM business.source WHERE id = 2',
    'SELECT a.id FROM business.source a JOIN business.customers b ON a.id=b.id'
)) OR (type IN ('QueryFinish','ExceptionWhileProcessing')
    AND hasAny(tables, ['business.source','business.customers'])))
GROUP BY query_id
ORDER BY min(query_start_time_microseconds), query_id
FORMAT TSVRaw;
SQL
    exit
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-clickhouse-decimal.XXXXXX")
CONTAINER_OWNED=false

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ] && [ "$MODE" = test ]; then
            docker logs "$CONTAINER_NAME" > "$WORK_DIR/clickhouse-container.log" 2>&1
            tail -n 200 "$WORK_DIR/clickhouse-container.log" >&2
        fi
        docker rm --force --volumes "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
        if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
            echo "ClickHouse gate left its owned container behind: $CONTAINER_NAME" >&2
            status=1
        fi
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
    --label "com.addp.online-fixture=$OWNER" \
    --publish 127.0.0.1::9000 \
    --env "CLICKHOUSE_DB=$DATABASE_NAME" \
    --env CLICKHOUSE_USER=default \
    --env "CLICKHOUSE_PASSWORD=$DATABASE_PASSWORD" \
    --env CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
    "$CLICKHOUSE_IMAGE" >/dev/null

ready=false
for _ in $(seq 1 60); do
    # The image runs a temporary server during initialization. Wait until PID 1
    # is the final server, otherwise its shutdown races the host-side tests.
    if docker exec "$CONTAINER_NAME" sh -c 'test "$(tr "\000" "\n" < /proc/1/cmdline | head -n 1)" = clickhouse-server' 2>/dev/null &&
        docker exec "$CONTAINER_NAME" clickhouse-client --user default --password "$DATABASE_PASSWORD" --query 'SELECT 1' 2>/dev/null | grep -Fxq '1'; then
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

if [ "$MODE" = online-start ]; then
    reader_password=$(openssl rand -hex 24)
    if ! admin_sql > /dev/null 2>"$WORK_DIR/seed-error.log" <<SQL
CREATE TABLE source (id UInt64, optional_text String) ENGINE = MergeTree ORDER BY id;
CREATE TABLE customers (id UInt64, optional_text String) ENGINE = MergeTree ORDER BY id;
INSERT INTO source VALUES (2, '');
INSERT INTO customers VALUES (2, 'fixture customer');
CREATE USER business_reader IDENTIFIED WITH sha256_password BY '$reader_password';
GRANT SELECT ON business.source TO business_reader;
GRANT SELECT ON business.customers TO business_reader;
SQL
    then
        sed "s/$reader_password/[redacted]/g" "$WORK_DIR/seed-error.log" >&2
        exit 1
    fi
    umask 077
    ADDP_CLICKHOUSE_READER_PASSWORD="$reader_password" python3 - "$host_port" "${ADDP_ONLINE_SECRET_DIR:?}/clickhouse-engine.json" <<'PYTHON'
import json
import os
from pathlib import Path
import sys

port, output = sys.argv[1:]
descriptor = {
    "engine_type": "clickhouse", "engine_origin": "general",
    "name": "ClickHouse isolated source",
    "description": "ClickHouse Develop source authorization fixture",
    "connection_info": {
        "host": "127.0.0.1", "port": int(port), "database": "business",
        "user": "business_reader", "password": os.environ["ADDP_CLICKHOUSE_READER_PASSWORD"],
    },
}
path = Path(output)
with path.open("x") as file:
    json.dump(descriptor, file)
PYTHON
    CONTAINER_OWNED=false
    echo 'ClickHouse Hosted source ready'
    exit
fi

export ADDP_CLICKHOUSE_INTEGRATION=1
export ADDP_TEST_CLICKHOUSE_HOST=127.0.0.1
export ADDP_TEST_CLICKHOUSE_PORT="$host_port"
export ADDP_TEST_CLICKHOUSE_DATABASE="$DATABASE_NAME"
export ADDP_TEST_CLICKHOUSE_USER=default
export ADDP_TEST_CLICKHOUSE_PASSWORD="$DATABASE_PASSWORD"

go -C "$ROOT_DIR/common" test ./engine/plugins/clickhouse \
    -run '^TestIntegrationClickHouse(BinaryTableRead|DecimalTableWriteDefinition|TypedJSONWrites|GuardedQueryRead)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-clickhouse-decimal.log"

go -C "$ROOT_DIR/manager/backend" test ./internal/api \
    -run '^TestIntegrationManagerClickHouseBinaryPreview$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/manager-clickhouse-preview.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-clickhouse-decimal.log" "$WORK_DIR/manager-clickhouse-preview.log" ||
    ! grep -q '^--- PASS: TestIntegrationClickHouseGuardedQueryRead ' "$WORK_DIR/common-clickhouse-decimal.log" ||
    ! grep -q '^--- PASS: TestIntegrationClickHouseBinaryTableRead ' "$WORK_DIR/common-clickhouse-decimal.log" ||
    ! grep -q '^--- PASS: TestIntegrationManagerClickHouseBinaryPreview ' "$WORK_DIR/manager-clickhouse-preview.log"; then
    echo "ClickHouse gate requires the guarded read and Provider/Manager binary regressions without skips" >&2
    exit 1
fi
