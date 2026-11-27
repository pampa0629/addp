#!/usr/bin/env bash
# common-doris-decimal-gate.sh - Verify Doris decimal and typed JSON writes against a disposable database.
# ADDP_T2_HOSTED_ONLY=doris

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CONTAINER_NAME=addp-doris-decimal-disposable
DORIS_IMAGE='apache/doris:doris-all-in-one-2.1.0@sha256:83538f071c8ea22134b8151d8b751cdf36da9e1a5da315443ba461909ee11835'
DATABASE_NAME=addp_doris_disposable
CONTAINER_OWNED=false
MODE=${1:-test}

case "$MODE" in
    test) ;;
    online-start|online-stop|online-audit)
        [ "${GITHUB_ACTIONS:-}/${RUNNER_OS:-}/${ADDP_ONLINE_HOST:-}/${ONLINE_SUITE_INPUT:-}" = "true/Linux/1/doris-consumer-flow" ] ||
            { echo "disposable Hosted Doris required" >&2; exit 1; }
        CONTAINER_NAME=addp-doris-source-online
        OWNER="doris-consumer-flow:${GITHUB_RUN_ID:?}:${GITHUB_RUN_ATTEMPT:?}"
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
    echo 'Doris Hosted source cleanup passed'
    exit 0
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-doris-decimal.XXXXXX")

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ] && [ "$MODE" = test ]; then
            docker logs "$CONTAINER_NAME" > "$WORK_DIR/doris-container.log" 2>&1
            tail -n 200 "$WORK_DIR/doris-container.log" >&2
        fi
        docker rm --force --volumes "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
        if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
            echo "Doris gate left its owned container behind: $CONTAINER_NAME" >&2
            status=1
        fi
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-common-doris-decimal.*) rm -rf "$WORK_DIR" ;;
    esac
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

snapshot_audit() {
    local marker
    marker=addp_audit_$(openssl rand -hex 12)
    docker exec "$CONTAINER_NAME" mysql -h127.0.0.1 -P9030 -uroot -N -B -e "SELECT 1 AS $marker" >/dev/null
    for _ in $(seq 1 25); do
        docker exec "$CONTAINER_NAME" cat /opt/apache-doris/fe/log/fe.audit.log > "$WORK_DIR/fe-audit.log"
        if grep -Fq "$marker" "$WORK_DIR/fe-audit.log"; then
            return
        fi
        sleep 1
    done
    echo "timed out waiting for the Doris FE audit marker" >&2
    return 1
}
if [ "$MODE" = online-audit ]; then
    check_owned
    snapshot_audit
    cat "$WORK_DIR/fe-audit.log"
    exit
fi

for command in docker go; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "$command is required for the disposable Doris decimal gate" >&2
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
    --label "com.addp.online-fixture=${OWNER:-doris-native-test}" \
    --publish 127.0.0.1::9030 \
    --env BUILD_TYPE=production \
    --entrypoint /bin/bash \
    "$DORIS_IMAGE" \
    -c "grep -q '^enable_fqdn_mode = true$' /opt/apache-doris/fe/conf/fe.conf || echo 'enable_fqdn_mode = true' >> /opt/apache-doris/fe/conf/fe.conf; grep -q '^force_olap_table_replication_num = 1$' /opt/apache-doris/fe/conf/fe.conf || echo 'force_olap_table_replication_num = 1' >> /opt/apache-doris/fe/conf/fe.conf; exec bash /usr/local/bin/entry_point.sh" >/dev/null

doris_has_ready_backend() {
    docker exec "$CONTAINER_NAME" \
        mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=5 --batch -e 'SHOW BACKENDS;' 2>/dev/null |
        awk -F '\t' '
            NR == 1 {
                for (column = 1; column <= NF; column++) {
                    if ($column == "AvailCapacity") available_column = column
                    if ($column == "Alive") alive_column = column
                    if ($column == "SystemDecommissioned") decommissioned_column = column
                }
                next
            }
            alive_column > 0 && decommissioned_column > 0 &&
            tolower($alive_column) == "true" &&
            tolower($decommissioned_column) == "false" && available_column > 0 && $available_column+0 > 0 {
                ready_backend = 1
            }
            END { exit !(alive_column > 0 && decommissioned_column > 0 && ready_backend == 1) }
        '
}

ready=false
for _ in $(seq 1 120); do
    if doris_has_ready_backend; then
        ready=true
        break
    fi
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "Doris disposable container exited before readiness" >&2
        exit 1
    fi
    sleep 5
done
if [ "$ready" != true ]; then
    echo "Doris disposable container did not expose an Alive backend within 600 seconds" >&2
    exit 1
fi

host_mapping=$(docker port "$CONTAINER_NAME" 9030/tcp)
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*)
        echo "cannot determine disposable Doris host port from: $host_mapping" >&2
        exit 1
        ;;
esac

seed_consumer() {
    reader_password=$(openssl rand -hex 24)
    docker exec -i "$CONTAINER_NAME" mysql -h127.0.0.1 -P9030 -uroot > /dev/null 2>"$WORK_DIR/seed-error.log" <<SQL
CREATE DATABASE business;
CREATE TABLE business.source (id INT NOT NULL, optional_text STRING NULL) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES('replication_num'='1');
CREATE TABLE business.customers (id INT NOT NULL, optional_text STRING NULL) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES('replication_num'='1');
INSERT INTO business.source VALUES (2, '');
INSERT INTO business.customers VALUES (2, 'fixture customer');
CREATE USER business_reader IDENTIFIED BY '$reader_password';
GRANT SELECT_PRIV ON business.source TO business_reader;
GRANT SELECT_PRIV ON business.customers TO business_reader;
SQL
}

if [ "$MODE" = online-start ]; then
    seed_consumer
    DORIS_PORT="$host_port" DORIS_READER_PASSWORD="$reader_password" python3 - <<'PYTHON'
import json, os
from pathlib import Path
path = Path(os.environ["ADDP_ONLINE_SECRET_DIR"], "doris-engine.json")
descriptor = dict(name="Hosted Doris read-only", engine_type="doris", engine_origin="general",
                  description="Disposable Doris SQL authorization source",
                  connection_info=dict(host="127.0.0.1", port=int(os.environ["DORIS_PORT"]), database="business",
                                       user="business_reader", password=os.environ["DORIS_READER_PASSWORD"]))
with open(path, "x", opener=lambda name, flags: os.open(name, flags, 0o600)) as stream:
    json.dump(descriptor, stream)
PYTHON
    CONTAINER_OWNED=false
    exit 0
fi

cd "$ROOT_DIR/common"
ADDP_DORIS_INTEGRATION=1 \
ADDP_TEST_DORIS_HOST=127.0.0.1 \
ADDP_TEST_DORIS_PORT="$host_port" \
ADDP_TEST_DORIS_DATABASE="$DATABASE_NAME" \
ADDP_TEST_DORIS_USER=root \
    go test ./engine/plugins/doris \
    -run '^TestIntegrationDoris(DecimalTableWriteDefinition|TypedJSONWrites|GuardedQueryReadSession)$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-doris-decimal.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-doris-decimal.log"; then
    echo "Common Doris decimal gate refuses skipped tests" >&2
    exit 1
fi

seed_consumer
[ "$(MYSQL_PWD="$reader_password" docker exec -e MYSQL_PWD "$CONTAINER_NAME" mysql -h127.0.0.1 -P9030 -ubusiness_reader \
    --batch --skip-column-names -e 'SELECT id FROM business.source WHERE id = 2')" = 2 ]
snapshot_audit
(cd "$ROOT_DIR" && python3 - "$WORK_DIR/fe-audit.log" <<'AUDIT'
from importlib import import_module
from pathlib import Path
import sys
flow = import_module('scripts.test.readonly-sql-consumer-flow-online')
audit = Path(sys.argv[1]).read_text()
if len(flow.audit_business_selects('', audit)) != 1:
    raise SystemExit('expected one native Doris SELECT in the disposable reader audit')
print('DORIS_NATIVE_BUSINESS_AUDIT_PASS')
AUDIT
)
