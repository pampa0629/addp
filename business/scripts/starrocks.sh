#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd -P)
source "$SCRIPT_DIR/ports.sh"
action=${1:-}
[[ "$#" = 1 && "$action" =~ ^(start|stop|status)$ ]] || { echo 'Usage: starrocks.sh start|stop|status' >&2; exit 1; }
cd "$PROJECT_ROOT"
if [ -f .env ]; then set -a; source .env; set +a; fi
export STARROCKS_PORT=${STARROCKS_PORT:-9031} STARROCKS_HTTP_PORT=${STARROCKS_HTTP_PORT:-8031} STARROCKS_BE_HTTP_PORT=${STARROCKS_BE_HTTP_PORT:-8041}
export STARROCKS_USER=${STARROCKS_USER:-business_reader} STARROCKS_PASSWORD=${STARROCKS_PASSWORD:-} STARROCKS_APP_PASSWORD=${STARROCKS_APP_PASSWORD:-}
compose() {
    local override
    override=$(addp_business_network_override)
    local args=(--project-name business --env-file /dev/null --file "$PROJECT_ROOT/docker-compose.yml")
    [ ! -f "$override" ] || args+=(--file "$override")
    docker compose "${args[@]}" --profile starrocks "$@"
}
if docker container inspect business-starrocks >/dev/null 2>&1; then
    ownership=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' business-starrocks)
    [ "$ownership" = "business|starrocks|$PROJECT_ROOT" ] || { echo 'Refusing a foreign StarRocks container.' >&2; exit 1; }
fi
case "$action" in
    stop) compose stop starrocks ;;
    status) compose ps starrocks ;;
    start)
        source "$PROJECT_ROOT/starrocks/entrypoint.sh"
        validate_config
        for port in "$STARROCKS_PORT" "$STARROCKS_HTTP_PORT" "$STARROCKS_BE_HTTP_PORT"; do
            [[ "$port" =~ ^[1-9][0-9]{3,4}$ ]] && [ "$port" -ge 1024 ] && [ "$port" -le 65535 ] || { echo 'Invalid StarRocks port.' >&2; exit 1; }
            if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
                docker port business-starrocks 2>/dev/null | awk '{print $NF}' | grep -Fxq "127.0.0.1:$port" || { echo "Port $port already belongs to another service." >&2; exit 1; }
            fi
        done
        [ "$STARROCKS_PORT" != "$STARROCKS_HTTP_PORT" ] && [ "$STARROCKS_PORT" != "$STARROCKS_BE_HTTP_PORT" ] && [ "$STARROCKS_HTTP_PORT" != "$STARROCKS_BE_HTTP_PORT" ] || { echo 'StarRocks ports must be distinct.' >&2; exit 1; }
        addp_business_ensure_network
        compose up -d starrocks
        for ((attempt=0; attempt<150; attempt++)); do
            health=$(docker inspect --format '{{.State.Health.Status}}' business-starrocks)
            if [ "$health" = healthy ]; then echo "StarRocks ready: 127.0.0.1:$STARROCKS_PORT/business, reader=$STARROCKS_USER"; exit 0; fi
            [ "$(docker inspect --format '{{.State.Running}}' business-starrocks)" = true ] || { echo 'StarRocks exited; persistent volumes were retained.' >&2; exit 1; }
            sleep 2
        done
        echo 'StarRocks readiness timed out; persistent volumes were retained.' >&2; exit 1 ;;
esac
