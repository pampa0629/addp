#!/usr/bin/env bash
# 独立 HighGo Business profile 生命周期；不登记 System 引擎，不删除数据卷。
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd -P)
source "$SCRIPT_DIR/ports.sh"

action=${1:-}
case "$action" in
    --help|-h) echo '用法: bash business/scripts/highgo.sh start|stop|status'; exit 0 ;;
    start|stop|status) ;;
    *) echo '需要 start、stop 或 status。' >&2; exit 1 ;;
esac
[ "$#" -eq 1 ] || { echo '只能指定一个操作。' >&2; exit 1; }
cd "$PROJECT_ROOT"
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi
export HIGHGO_PORT=${HIGHGO_PORT:-5866}
[[ "$HIGHGO_PORT" =~ ^[1-9][0-9]{3,4}$ ]] && [ "$HIGHGO_PORT" -le 65535 ] || { echo 'HIGHGO_PORT 必须是 1024 至 65535 的端口。' >&2; exit 1; }
[ "$HIGHGO_PORT" -ge 1024 ] || exit 1

compose() {
    local override
    override=$(addp_business_network_override)
    local args=(--project-name business --env-file /dev/null --file "$PROJECT_ROOT/docker-compose.yml")
    [ ! -f "$override" ] || args+=(--file "$override")
    docker compose "${args[@]}" --profile highgo "$@"
}

if docker container inspect business-highgo >/dev/null 2>&1; then
    ownership=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' business-highgo)
    [ "$ownership" = "business|highgo|$PROJECT_ROOT" ] || { echo 'business-highgo 不属于本工作区。' >&2; exit 1; }
fi

case "$action" in
    stop) compose stop highgo ;;
    status) compose ps highgo ;;
    start)
        [ -n "${HIGHGO_PASSWORD:-}" ] || { echo '请在 business/.env 设置 HIGHGO_PASSWORD。' >&2; exit 1; }
        [[ "$HIGHGO_PASSWORD" != *$'\n'* && "$HIGHGO_PASSWORD" != *$'\r'* ]] || { echo 'HIGHGO_PASSWORD 不得包含换行。' >&2; exit 1; }
        if lsof -nP -iTCP:"$HIGHGO_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
            actual=$(docker port business-highgo 5866/tcp 2>/dev/null || true)
            [ "$actual" = "127.0.0.1:$HIGHGO_PORT" ] || { echo "端口 $HIGHGO_PORT 已被其他服务占用。" >&2; exit 1; }
        fi
        addp_business_ensure_network
        compose up -d highgo
        # Compose 健康检查使用配置密码和 TLS；不将密码写入命令输出。
        for ((attempt=0; attempt<60; attempt++)); do
            health=$(docker inspect --format '{{.State.Health.Status}}' business-highgo)
            if [ "$health" = healthy ]; then
                echo "瀚高已就绪：127.0.0.1:$HIGHGO_PORT/highgo，schema=addp_business，user=sysdba，sslmode=require"
                exit 0
            fi
            [ "$(docker inspect --format '{{.State.Running}}' business-highgo)" = true ] || { echo '瀚高容器已退出，请检查日志。' >&2; exit 1; }
            sleep 2
        done
        echo '瀚高健康检查超时，请检查初始化、口令策略和 License。' >&2
        exit 1
        ;;
esac
