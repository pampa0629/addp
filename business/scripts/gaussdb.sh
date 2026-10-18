#!/usr/bin/env bash
# Persistent Business GaussDB; no platform API calls and no volume deletion.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd -P)
ROOT_DIR=$(cd "$PROJECT_ROOT/.." && pwd -P)
source "$SCRIPT_DIR/ports.sh"
source "$ROOT_DIR/scripts/lib/gaussdb-official-media.sh"
action=${1:-}
case "$action" in
    --help|-h) echo '用法: bash business/scripts/gaussdb.sh start|stop|status'; exit 0 ;;
    start|stop|status) ;;
    *) echo '需要 start、stop 或 status。' >&2; exit 1 ;;
esac
[ "$#" = 1 ] || exit 1
cd "$PROJECT_ROOT"
if [ -f .env ]; then set -a; source .env; set +a; fi
export GAUSSDB_PORT=${GAUSSDB_PORT:-5437} GAUSSDB_USER=${GAUSSDB_USER:-business}
[[ "$GAUSSDB_PORT" =~ ^[1-9][0-9]{3,4}$ ]] && [ "$GAUSSDB_PORT" -ge 1024 ] && [ "$GAUSSDB_PORT" -le 65535 ] || {
    echo 'GAUSSDB_PORT 必须是1024至65535的端口。' >&2; exit 1;
}
compose() {
    local override args
    override=$(addp_business_network_override)
    args=(--project-name business --env-file /dev/null --file "$PROJECT_ROOT/docker-compose.yml")
    [ ! -f "$override" ] || args+=(--file "$override")
    docker compose "${args[@]}" --profile gaussdb "$@"
}
if docker container inspect business-gaussdb >/dev/null 2>&1; then
    ownership=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' business-gaussdb)
    [ "$ownership" = "business|gaussdb|$PROJECT_ROOT" ] || { echo 'business-gaussdb 不属于本工作区。' >&2; exit 1; }
fi
case "$action" in
    stop) compose stop gaussdb ;;
    status) compose ps gaussdb ;;
    start)
        # Source validation only; never source the container lifecycle as main.
        source "$PROJECT_ROOT/gaussdb/entrypoint.sh"
        validate_config
        export GAUSSDB_PASSWORD GAUSSDB_APP_PASSWORD
        if lsof -nP -iTCP:"$GAUSSDB_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
            [ "$(docker port business-gaussdb 5432/tcp 2>/dev/null || true)" = "127.0.0.1:$GAUSSDB_PORT" ] || {
                echo "端口 $GAUSSDB_PORT 已被其他服务占用。" >&2; exit 1;
            }
        fi
        context=$(mktemp -d "${TMPDIR:-/tmp}/addp-gaussdb-build.XXXXXX")
        trap 'rm -rf "$context"' EXIT
        python3 "$PROJECT_ROOT/gaussdb/image.py" --context "$context"
        docker build --build-arg "GAUSSDB_BASE_IMAGE=$GAUSSDB_OFFICIAL_BASE_IMAGE" --build-arg "GAUSSDB_VERSION=$GAUSSDB_OFFICIAL_VERSION" -t addp/gaussdb:developer-10.0.0 "$context"
        addp_business_ensure_network
        compose up -d gaussdb
        for ((attempt=0; attempt<120; attempt++)); do
            health=$(docker inspect --format '{{.State.Health.Status}}' business-gaussdb)
            if [ "$health" = healthy ]; then
                echo "GaussDB 已就绪：127.0.0.1:$GAUSSDB_PORT/business，schema=addp_business，user=$GAUSSDB_USER"
                exit 0
            fi
            [ "$(docker inspect --format '{{.State.Running}}' business-gaussdb)" = true ] || {
                echo 'GaussDB 已退出，请检查日志；不会删除或重建数据卷。' >&2; exit 1;
            }
            sleep 2
        done
        echo 'GaussDB 健康检查超时。' >&2; exit 1
        ;;
esac
