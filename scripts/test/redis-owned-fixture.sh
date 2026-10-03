#!/usr/bin/env bash
# Shared lifecycle for the two Redis disposable consumer gates; never uses existing services.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export ADDP_REDIS_T2_PROJECT="addp-redis-t2-${PPID}-$$"
export BUSINESS_REDIS_ADMIN_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
export BUSINESS_REDIS_READER_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
compose() { docker compose -p "$ADDP_REDIS_T2_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.redis-t2.yml" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    local containers volumes networks
    containers=$(docker ps -a --filter "label=com.docker.compose.project=$ADDP_REDIS_T2_PROJECT" --format '{{.ID}}') || status=1
    volumes=$(docker volume ls --filter "label=com.docker.compose.project=$ADDP_REDIS_T2_PROJECT" --format '{{.Name}}') || status=1
    networks=$(docker network ls --filter "label=com.docker.compose.project=$ADDP_REDIS_T2_PROJECT" --format '{{.ID}}') || status=1
    if [ -n "$containers$volumes$networks" ]; then
        echo "Redis T2 cleanup left owned resources" >&2
        status=1
    fi
    exit "$status"
}
trap cleanup EXIT INT TERM
compose up -d --wait --wait-timeout 120
compose exec -T redis sh /addp/redis/init.sh
compose exec -T redis sh /addp/redis/init.sh
export ADDP_REDIS_T2_ENDPOINT="$(compose port redis 6379)"
"$@"
