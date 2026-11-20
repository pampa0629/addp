#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
export COMPOSE_PROJECT_NAME="nb-gate-$(python3 -c 'import uuid; print(uuid.uuid4().hex[:12])')"
export JUPYTER_TEST_IMAGE="${JUPYTER_TEST_IMAGE:-localhost:5001/addp-jupyter-engine:latest}"
compose=(docker compose --env-file /dev/null -p "$COMPOSE_PROJECT_NAME" -f docker-compose.runtimes.yml -f scripts/test/docker-compose.jupyter-isolation.yml)
cleanup() {
    local result=$?
    trap - EXIT
    if [ "$result" -ne 0 ]; then
        "${compose[@]}" logs --tail 25 jupyter-launcher jupyter-engine >&2 || true
    fi
    "${compose[@]}" down --volumes || result=1
    local label="label=addp.jupyter.launcher=$COMPOSE_PROJECT_NAME"
    if [ -n "$(docker ps -aq --filter "$label")$(docker network ls -q --filter "$label")" ]; then
        echo "Notebook container/network cleanup incomplete" >&2
        result=1
    fi
    exit "$result"
}
trap cleanup EXIT
"${compose[@]}" up -d --wait --wait-timeout 60 --no-build --pull never jupyter-engine
"${compose[@]}" exec -T jupyter-engine python /fixture/probe.py check
