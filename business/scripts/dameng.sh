#!/usr/bin/env bash
# dameng.sh - Manage the local DM8 ARM64 official-media technical fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
# shellcheck source=../../scripts/lib/dameng-official-media.sh
source "$ROOT_DIR/scripts/lib/dameng-official-media.sh"

CONTAINER_NAME=business-dameng
COMPOSE_FILE=$ROOT_DIR/business/docker-compose.yml
COMPOSE_PROJECT=business
COMPOSE_SERVICE=dameng
HOST_PORT=${DAMENG_PORT:-5236}

fail() {
    echo "Business DM8 failed: $*" >&2
    exit 1
}

compose() {
    docker compose --project-name "$COMPOSE_PROJECT" --env-file /dev/null --file "$COMPOSE_FILE" --profile dameng "$@"
}

action=${1:-}
case "$action" in
    start|stop|status) ;;
    --help|-h)
        echo "usage: bash business/scripts/dameng.sh start|stop|status"
        exit 0
        ;;
    *) fail "usage: bash business/scripts/dameng.sh start|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"
[[ "$HOST_PORT" =~ ^[0-9]+$ ]] && [ "$HOST_PORT" -ge 1024 ] && [ "$HOST_PORT" -le 65535 ] ||
    fail "DAMENG_PORT must be between 1024 and 65535"

container_exists() {
    docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1
}

container_running() {
    [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" = "true" ]
}

validate_container_ownership() {
    if container_exists; then
        local ownership
        ownership=$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}/{{ index .Config.Labels "com.addp.business-fixture" }}' "$CONTAINER_NAME")
        [ "$ownership" = "$COMPOSE_PROJECT/$COMPOSE_SERVICE/dameng" ] ||
            fail "$CONTAINER_NAME is not owned by the Business DM8 Compose service"
    fi
}

run_sql_file() {
    local sql_file=$1
    local output

    output=$(dameng_disql "$CONTAINER_NAME" < "$sql_file" 2>&1) || {
        printf '%s\n' "$output" >&2
        return 1
    }
    if printf '%s\n' "$output" | grep -Eiq 'error\[|\[-[0-9]+\]'; then
        printf '%s\n' "$output" >&2
        return 1
    fi
}

query_contains() {
    local sql=$1
    local expected=$2
    local output

    output=$(printf '%s\nEXIT\n' "$sql" | dameng_disql "$CONTAINER_NAME" 2>&1) || return 1
    if printf '%s\n' "$output" | grep -Eiq 'error\[|\[-[0-9]+\]'; then
        printf '%s\n' "$output" >&2
        return 1
    fi
    printf '%s\n' "$output" | grep -Fq "$expected"
}

initialize_sample() {
    run_sql_file "$ROOT_DIR/business/dameng/init.sql"
    query_contains "SELECT ENGINE_NAME FROM ADDP_ENGINE_PROBE WHERE ID = 1;" "DM8 ARM64 technical fixture" ||
        fail "DM8 Business sample probe is missing"
    query_contains "SELECT COUNT(*) FROM ADDP_RELATIONAL_SAMPLE;" "3" ||
        fail "DM8 Business relational sample is incomplete"
}

validate_container_ownership

case "$action" in
    start)
        dameng_ensure_official_image "$ROOT_DIR"
        export DAMENG_PORT="$HOST_PORT"
        if ! container_running; then
            if container_exists; then
                compose rm --force "$COMPOSE_SERVICE" >/dev/null
            fi
            compose create "$COMPOSE_SERVICE" >/dev/null
            validate_container_ownership
            compose start "$COMPOSE_SERVICE" >/dev/null || {
                compose rm --force "$COMPOSE_SERVICE" >/dev/null 2>&1 || true
                fail "failed to start the DM8 Business Compose service"
            }
        fi
        for _ in $(seq 1 120); do
            if container_running && dameng_container_ready "$CONTAINER_NAME"; then
                initialize_sample
                echo "Business DM8 ARM64 technical fixture is ready on 127.0.0.1:$HOST_PORT"
                echo "This local trial fixture is not an ADDP engine registration and expires on 2027-07-07."
                exit 0
            fi
            container_running || fail "DM8 Business container exited before readiness"
            sleep 2
        done
        fail "DM8 Business container did not become ready within 240 seconds"
        ;;
    stop)
        if container_exists; then
            compose rm --stop --force "$COMPOSE_SERVICE" >/dev/null
        fi
        container_exists && fail "$CONTAINER_NAME still exists after cleanup"
        echo "Business DM8 technical fixture is stopped with zero container residue"
        ;;
    status)
        container_running || fail "$CONTAINER_NAME is not running"
        dameng_container_ready "$CONTAINER_NAME" || fail "DM8 Business database is not ready"
        query_contains "SELECT ENGINE_NAME FROM ADDP_ENGINE_PROBE WHERE ID = 1;" "DM8 ARM64 technical fixture" ||
            fail "DM8 Business sample probe is missing"
        echo "Business DM8 ARM64 technical fixture is ready"
        ;;
esac
