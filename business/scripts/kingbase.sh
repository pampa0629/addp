#!/usr/bin/env bash
# kingbase.sh - Manage the licensed disposable KingbaseES Business sample on Linux x86_64.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
# shellcheck source=../../scripts/lib/kingbase-official-media.sh
source "$ROOT_DIR/scripts/lib/kingbase-official-media.sh"

CONTAINER_NAME=business-kingbase
COMPOSE_FILE=$ROOT_DIR/business/docker-compose.yml
COMPOSE_PROJECT=business
COMPOSE_SERVICE=kingbase
DATABASE_NAME=${KINGBASE_DATABASE:-business}
DATABASE_USER=${KINGBASE_USER:-system}
DATABASE_PASSWORD=${KINGBASE_PASSWORD:-}
HOST_PORT=${KINGBASE_PORT:-5436}
WORK_DIR=${TMPDIR:-/tmp}/addp-business-kingbase

fail() {
    echo "Business KingbaseES failed: $*" >&2
    exit 1
}

compose() {
    docker compose --project-name "$COMPOSE_PROJECT" --env-file /dev/null --file "$COMPOSE_FILE" --profile kingbase "$@"
}

action=${1:-}
case "$action" in
    start|stop|status) ;;
    --help|-h)
        echo "usage: bash business/scripts/kingbase.sh start|stop|status"
        exit 0
        ;;
    *) fail "usage: bash business/scripts/kingbase.sh start|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

[[ "$DATABASE_NAME" =~ ^[A-Za-z][A-Za-z0-9_]{0,62}$ ]] || fail "KINGBASE_DATABASE must be a safe identifier"
[[ "$DATABASE_USER" =~ ^[A-Za-z][A-Za-z0-9_]{0,62}$ ]] || fail "KINGBASE_USER must be a safe identifier"
[[ "$HOST_PORT" =~ ^[0-9]+$ ]] && [ "$HOST_PORT" -ge 1024 ] && [ "$HOST_PORT" -le 65535 ] || fail "KINGBASE_PORT must be between 1024 and 65535"

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
        [ "$ownership" = "$COMPOSE_PROJECT/$COMPOSE_SERVICE/kingbase" ] ||
            fail "$CONTAINER_NAME is not owned by the Business KingbaseES Compose service"
    fi
}

kingbase_database_exists() {
    kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -At -c \
        "SELECT 1 FROM pg_database WHERE datname = '$DATABASE_NAME'" | grep -Fxq 1
}

initialize_sample() {
    if ! kingbase_database_exists; then
        kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 -c \
            "CREATE DATABASE \"$DATABASE_NAME\""
    fi
    docker exec --interactive "$CONTAINER_NAME" "$KINGBASE_HOME/bin/ksql" \
        -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 \
        < "$ROOT_DIR/business/kingbase/init.sql" >/dev/null
    kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -At -c \
        'SELECT engine_name FROM addp_engine_probe WHERE id = 1' | grep -Fxq 'KingbaseES V9R1C10' ||
        fail "KingbaseES Business sample probe is missing"
}

validate_container_ownership

case "$action" in
    start)
        [ -n "$DATABASE_PASSWORD" ] || fail "KINGBASE_PASSWORD is required"
        kingbase_validate_license_input "$ROOT_DIR"
        kingbase_ensure_official_image
        export KINGBASE_USER="$DATABASE_USER"
        export KINGBASE_PASSWORD="$DATABASE_PASSWORD"
        export KINGBASE_PORT="$HOST_PORT"
        if ! container_running; then
            mkdir -p "$WORK_DIR"
            chmod 0700 "$WORK_DIR"
            if container_exists; then
                compose rm --force "$COMPOSE_SERVICE" >/dev/null
            fi
            compose create "$COMPOSE_SERVICE" >/dev/null
            validate_container_ownership
            if ! kingbase_install_license_into_created_container "$CONTAINER_NAME" "$ADDP_KINGBASE_LICENSE_FILE" "$WORK_DIR"; then
                compose rm --force "$COMPOSE_SERVICE" >/dev/null 2>&1 || true
                rmdir "$WORK_DIR" 2>/dev/null || true
                fail "failed to inject the owner-managed KingbaseES License"
            fi
            rmdir "$WORK_DIR" 2>/dev/null || true
            if ! compose start "$COMPOSE_SERVICE" >/dev/null; then
                compose rm --force "$COMPOSE_SERVICE" >/dev/null 2>&1 || true
                fail "failed to start the KingbaseES Business Compose service"
            fi
        fi
        for _ in $(seq 1 120); do
            if container_running && kingbase_container_ready "$CONTAINER_NAME" "$DATABASE_USER"; then
                initialize_sample
                echo "Business KingbaseES sample is ready on 127.0.0.1:$HOST_PORT"
                exit 0
            fi
            container_running || fail "KingbaseES Business container exited before readiness"
            sleep 5
        done
        fail "KingbaseES Business container did not become ready within 600 seconds"
        ;;
    stop)
        if container_exists; then
            compose rm --stop --force "$COMPOSE_SERVICE" >/dev/null
        fi
        rmdir "$WORK_DIR" 2>/dev/null || true
        container_exists && fail "$CONTAINER_NAME still exists after cleanup"
        echo "Business KingbaseES sample is stopped with zero container residue"
        ;;
    status)
        container_running || fail "$CONTAINER_NAME is not running"
        kingbase_container_ready "$CONTAINER_NAME" "$DATABASE_USER" || fail "KingbaseES Business database is not ready"
        echo "Business KingbaseES sample is ready"
        ;;
esac
