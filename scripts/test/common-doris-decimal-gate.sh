#!/usr/bin/env bash
# common-doris-decimal-gate.sh - Verify Doris decimal table-write precision against a disposable database.
# ADDP_T2_HOSTED_ONLY=doris

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CONTAINER_NAME=addp-doris-decimal-disposable
DORIS_IMAGE='apache/doris:doris-all-in-one-2.1.0@sha256:83538f071c8ea22134b8151d8b751cdf36da9e1a5da315443ba461909ee11835'
DATABASE_NAME=addp_doris_disposable
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-doris-decimal.XXXXXX")
CONTAINER_OWNED=false

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "$WORK_DIR/doris-container.log" 2>&1
            tail -n 200 "$WORK_DIR/doris-container.log" >&2
        fi
        docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-common-doris-decimal.*) rm -rf "$WORK_DIR" ;;
    esac
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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
    --publish 127.0.0.1::9030 \
    --env BUILD_TYPE=production \
    --entrypoint /bin/bash \
    "$DORIS_IMAGE" \
    -c "grep -q '^enable_fqdn_mode = true$' /opt/apache-doris/fe/conf/fe.conf || echo 'enable_fqdn_mode = true' >> /opt/apache-doris/fe/conf/fe.conf; grep -q '^force_olap_table_replication_num = 1$' /opt/apache-doris/fe/conf/fe.conf || echo 'force_olap_table_replication_num = 1' >> /opt/apache-doris/fe/conf/fe.conf; exec bash /usr/local/bin/entry_point.sh" >/dev/null

ready=false
for _ in $(seq 1 120); do
    if docker exec "$CONTAINER_NAME" mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=5 -e 'SHOW FRONTENDS;' >/dev/null 2>&1; then
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
    echo "Doris disposable container did not become ready within 600 seconds" >&2
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

cd "$ROOT_DIR/common"
ADDP_DORIS_INTEGRATION=1 \
ADDP_TEST_DORIS_HOST=127.0.0.1 \
ADDP_TEST_DORIS_PORT="$host_port" \
ADDP_TEST_DORIS_DATABASE="$DATABASE_NAME" \
ADDP_TEST_DORIS_USER=root \
    go test ./engine/plugins/doris \
    -run '^TestIntegrationDorisDecimalTableWriteDefinition$' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-doris-decimal.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-doris-decimal.log"; then
    echo "Common Doris decimal gate refuses skipped tests" >&2
    exit 1
fi
