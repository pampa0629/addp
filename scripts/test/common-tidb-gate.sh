#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=tidb-pd,tidb-tikv,tidb
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.tidb-t2.yml
# common-tidb-gate.sh - Run TiDB Engine Provider integration tests against a disposable database.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-common-tidb.XXXXXX")
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.tidb-t2.yml"
COMPOSE_PROJECT="addp-tidb-t2-${PPID}-$$"
TIDB_T2_PORT=${TIDB_T2_PORT:-14000}

compose() {
    TIDB_T2_PORT="$TIDB_T2_PORT" docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"
}

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    if [ "$status" -ne 0 ]; then
        echo "Common TiDB gate Compose state:" >&2
        compose ps -a >&2
        echo "Common TiDB gate container logs:" >&2
        compose logs --no-color --tail=200 >&2
    fi
    compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    if docker ps -a --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q .; then
        echo "Common TiDB gate cleanup left Compose containers" >&2
        status=1
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT INT TERM

export ADDP_TEST_TIDB_HOST=127.0.0.1
export ADDP_TEST_TIDB_PORT="$TIDB_T2_PORT"
export ADDP_TEST_TIDB_USER=root
export ADDP_TEST_TIDB_PASSWORD=
export ADDP_TEST_TIDB_DATABASE=${ADDP_TEST_TIDB_DATABASE:-addp_tidb_disposable}

database=${ADDP_TEST_TIDB_DATABASE:-addp_tidb_disposable}
case "$database" in
    *disposable*) ;;
    *)
        echo "ADDP_TEST_TIDB_DATABASE must identify an isolated disposable database" >&2
        exit 1
        ;;
esac

cd "$ROOT_DIR/common"
compose up -d --force-recreate tidb-pd tidb-tikv tidb
ADDP_TEST_TIDB_DATABASE="$database" \
ADDP_TIDB_INTEGRATION=1 \
    go test ./engine/plugins/tidb \
    -run '^TestIntegrationTiDB' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/common-tidb.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/common-tidb.log"; then
    echo "Common TiDB gate refuses skipped tests" >&2
    exit 1
fi

docker run --rm --network "${COMPOSE_PROJECT}_default" \
    mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b \
    mysql -htidb -P4000 -uroot --protocol=tcp --connect-timeout=10 \
    -Nse "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = '$database'" |
    grep -qx '0' || {
        echo "Common TiDB gate left disposable database $database" >&2
        exit 1
    }
