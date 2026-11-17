#!/usr/bin/env bash
# Shared lifecycle for Redis Business, observation and core safety gates; never uses existing services.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export ADDP_REDIS_T2_PROJECT="addp-redis-t2-${PPID}-$$"
export BUSINESS_REDIS_ADMIN_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
export BUSINESS_REDIS_READER_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
compose() { docker compose --profile redis-observation -p "$ADDP_REDIS_T2_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.redis-t2.yml" "$@"; }
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
    if [ "$status" = 0 ]; then echo "Redis disposable containers, networks and volumes: zero residue"; fi
    exit "$status"
}
trap cleanup EXIT INT TERM
if [[ "${ADDP_REDIS_CORE_T2:-0}" == 1 ]]; then
  compose up -d --wait --wait-timeout 120 redis-infra
  export ADDP_REDIS_CORE_ENDPOINT="$(compose port redis-infra 6379)"
  export ADDP_REDIS_CORE_PASSWORD="$BUSINESS_REDIS_ADMIN_PASSWORD"
else
  services=(redis)
  if [[ "${ADDP_REDIS_OBSERVATION_T2:-0}" == 1 ]]; then services+=(redis-infra); fi
  compose up -d --wait --wait-timeout 120 "${services[@]}"
  compose exec -T redis sh /addp/redis/init.sh
  compose exec -T redis sh /addp/redis/init.sh
  export ADDP_REDIS_T2_ENDPOINT="$(compose port redis 6379)"
fi
"$@"
