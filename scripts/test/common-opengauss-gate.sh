#!/usr/bin/env bash
# common-opengauss-gate.sh - Run the hosted-only openGauss Provider contract.
# ADDP_T2_HOSTED_ONLY=opengauss

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
# shellcheck source=../lib/opengauss-official-media.sh
source "$ROOT_DIR/scripts/lib/opengauss-official-media.sh"

CONTAINER_NAME=addp-opengauss-provider-disposable
DATABASE_NAME=addp_opengauss_disposable
DATABASE_USER=gaussdb
DATABASE_PASSWORD='AddpGauss606@'
OPENGAUSS_HOME=/usr/local/opengauss
OPENGAUSS_EXEC_PATH=/usr/local/opengauss/bin:/scws/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
OPENGAUSS_LIBRARY_PATH=/usr/local/opengauss/lib:/scws/lib
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-opengauss.XXXXXX")
CONTAINER_OWNED=false

opengauss_gsql() {
    docker exec --user omm \
        --env "GAUSSHOME=$OPENGAUSS_HOME" \
        --env "PATH=$OPENGAUSS_EXEC_PATH" \
        --env "LD_LIBRARY_PATH=$OPENGAUSS_LIBRARY_PATH" \
        "$CONTAINER_NAME" "$OPENGAUSS_HOME/bin/gsql" "$@"
}

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if [ "$CONTAINER_OWNED" = true ] && docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
        if [ "$status" -ne 0 ]; then
            docker logs "$CONTAINER_NAME" > "$WORK_DIR/opengauss-container.log" 2>&1
            echo "openGauss container log (last 200 lines):" >&2
            tail -n 200 "$WORK_DIR/opengauss-container.log" >&2
        fi
        docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || status=1
    fi
    if [ "$status" -ne 0 ] && [ -f "$WORK_DIR/common-opengauss.log" ]; then
        echo "Common openGauss Go test log (last 120 lines):" >&2
        tail -n 120 "$WORK_DIR/common-opengauss.log" >&2
    fi
    case "$WORK_DIR" in
        "${TMPDIR:-/tmp}"/addp-common-opengauss.*) rm -rf "$WORK_DIR" ;;
    esac
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

for command in docker go; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "$command is required for the disposable openGauss gate" >&2
        exit 1
    fi
done
if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    echo "refusing to replace existing container: $CONTAINER_NAME" >&2
    exit 1
fi

if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "x86_64" ]; then
    echo "openGauss 6.0.6 official Docker media requires a Linux x86_64 runtime with NUMA; run this gate in GitHub Actions" >&2
    exit 1
fi

opengauss_ensure_official_image x86_64

export GS_PASSWORD=$DATABASE_PASSWORD
CONTAINER_OWNED=true
docker run --detach \
    --name "$CONTAINER_NAME" \
    --privileged=true \
    --publish 127.0.0.1::5432 \
    --env GS_PASSWORD \
    "$OPENGAUSS_OFFICIAL_IMAGE" >/dev/null
unset GS_PASSWORD

ready=false
for _ in $(seq 1 120); do
    if opengauss_official_container_ready "$CONTAINER_NAME" opengauss_gsql; then
        ready=true
        break
    fi
    if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]; then
        echo "openGauss disposable container exited before readiness" >&2
        exit 1
    fi
    sleep 5
done
if [ "$ready" != true ]; then
    echo "openGauss disposable container did not become ready within 600 seconds" >&2
    exit 1
fi

opengauss_gsql -d postgres -p 5432 -c "CREATE DATABASE $DATABASE_NAME DBCOMPATIBILITY 'PG'"
host_mapping=$(docker port "$CONTAINER_NAME" 5432/tcp)
host_port=${host_mapping##*:}
case "$host_port" in
    ''|*[!0-9]*)
        echo "cannot determine disposable openGauss host port from: $host_mapping" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/common"
ADDP_OPENGAUSS_INTEGRATION=1 \
ADDP_TEST_OPENGAUSS_HOST=127.0.0.1 \
ADDP_TEST_OPENGAUSS_PORT="$host_port" \
ADDP_TEST_OPENGAUSS_DATABASE="$DATABASE_NAME" \
ADDP_TEST_OPENGAUSS_USER="$DATABASE_USER" \
ADDP_TEST_OPENGAUSS_PASSWORD="$DATABASE_PASSWORD" \
    go test ./engine/plugins/opengauss \
    -run '^TestIntegrationOpenGauss' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-opengauss.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-opengauss.log"; then
    echo "Common openGauss gate refuses skipped tests" >&2
    exit 1
fi
