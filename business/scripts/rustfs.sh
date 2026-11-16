#!/usr/bin/env bash
# rustfs.sh - 管理独立 Business RustFS，保留数据卷，不调用平台 API。
# 用法: bash business/scripts/rustfs.sh start|stop|status
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd -P)
source "$SCRIPT_DIR/ports.sh"
action=${1:-}
[[ "$#" = 1 && "$action" =~ ^(start|stop|status)$ ]] || { echo '用法: rustfs.sh start|stop|status' >&2; exit 1; }
cd "$PROJECT_ROOT"
if [ -f .env ]; then set -a; source .env; set +a; fi
export RUSTFS_API_PORT=${RUSTFS_API_PORT:-9004} RUSTFS_CONSOLE_PORT=${RUSTFS_CONSOLE_PORT:-9005}
export RUSTFS_ACCESS_KEY=${RUSTFS_ACCESS_KEY:-} RUSTFS_SECRET_KEY=${RUSTFS_SECRET_KEY:-}
compose() {
    local override
    override=$(addp_business_network_override)
    local args=(--project-name business --env-file /dev/null --file "$PROJECT_ROOT/docker-compose.yml")
    [ ! -f "$override" ] || args+=(--file "$override")
    docker compose "${args[@]}" --profile rustfs "$@"
}
if docker container inspect business-rustfs >/dev/null 2>&1; then
    ownership=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' business-rustfs)
    [ "$ownership" = "business|rustfs|$PROJECT_ROOT" ] || { echo '拒绝操作其他工作区的 RustFS 容器。' >&2; exit 1; }
fi
case "$action" in
    stop) compose stop rustfs ;;
    status) compose ps rustfs ;;
    start)
        [[ -n "$RUSTFS_ACCESS_KEY" && ${#RUSTFS_SECRET_KEY} -ge 12 && "$RUSTFS_ACCESS_KEY" != rustfsadmin && "$RUSTFS_SECRET_KEY" != rustfsadmin ]] || {
            echo '请设置独立的 RustFS Access Key 和至少 12 字符的 Secret Key，禁止公开默认凭据。' >&2; exit 1;
        }
        for port in "$RUSTFS_API_PORT" "$RUSTFS_CONSOLE_PORT"; do
            [[ "$port" =~ ^[1-9][0-9]{3,4}$ ]] && [ "$port" -ge 1024 ] && [ "$port" -le 65535 ] || { echo 'RustFS 端口必须为 1024–65535。' >&2; exit 1; }
        done
        [ "$RUSTFS_API_PORT" != "$RUSTFS_CONSOLE_PORT" ] || { echo 'RustFS API 和 Console 端口必须不同。' >&2; exit 1; }
        addp_business_ensure_network
        # Compose 对显式端口冲突直接失败；不探测或接管占用者。
        compose up -d --wait --wait-timeout 150 rustfs
        echo "RustFS API: http://127.0.0.1:$RUSTFS_API_PORT"
        echo "RustFS Console: http://127.0.0.1:$RUSTFS_CONSOLE_PORT"
        ;;
esac
