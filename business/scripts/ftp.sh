#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd -P)
source "$SCRIPT_DIR/ports.sh"
action=${1:-}
[[ "$#" = 1 && "$action" =~ ^(start|stop|status)$ ]] || { echo 'Usage: ftp.sh start|stop|status' >&2; exit 1; }
cd "$PROJECT_ROOT"
if [ -f .env ]; then set -a; source .env; set +a; fi
export FTP_USER=${FTP_USER:-business_reader} FTP_PASSWORD=${FTP_PASSWORD:-} FTP_SECURITY=${FTP_SECURITY:-explicit_tls}
compose() {
    local override
    override=$(addp_business_network_override)
    local args=(--project-name business --env-file /dev/null --file "$PROJECT_ROOT/docker-compose.yml")
    [ ! -f "$override" ] || args+=(--file "$override")
    docker compose "${args[@]}" --profile ftp "$@"
}
if docker container inspect business-ftp >/dev/null 2>&1; then
    ownership=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' business-ftp)
    [ "$ownership" = "business|ftp|$PROJECT_ROOT" ] || { echo 'Refusing a foreign FTP container.' >&2; exit 1; }
fi
case "$action" in
    stop) compose stop ftp ;;
    status) compose ps ftp ;;
    start)
        python3 "$PROJECT_ROOT/ftp/prepare.py" prepare
        for port in 2121 {50000..50009}; do
            if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
                docker port business-ftp 2>/dev/null | grep -Eq "^([0-9]+/tcp -> )?127\.0\.0\.1:$port$" || {
                    echo "FTP port $port belongs to another service." >&2; exit 1;
                }
            fi
        done
        addp_business_ensure_network
        compose up -d ftp
        for ((attempt=0; attempt<60; attempt++)); do
            if python3 "$PROJECT_ROOT/ftp/prepare.py" probe 2>/dev/null; then
                echo 'FTP ready: 127.0.0.1:2121; passive ports 50000-50009; CA: business/.business-state/ftp/config/server.crt'
                exit 0
            fi
            [ "$(docker inspect --format '{{.State.Running}}' business-ftp)" = true ] || {
                echo 'FTP exited; host files and account state were retained.' >&2; exit 1;
            }
            sleep 1
        done
        echo 'FTP readiness timed out; host files and account state were retained.' >&2; exit 1 ;;
esac
