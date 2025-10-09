#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  source "${ROOT_DIR}/.env"
  set +a
fi

export POSTGRES_HOST=${POSTGRES_HOST:-localhost}
export POSTGRES_PORT=${POSTGRES_PORT:-5432}
export REDIS_HOST=${REDIS_HOST:-localhost}
export REDIS_PORT=${REDIS_PORT:-6379}
export SYSTEM_SERVICE_URL=${SYSTEM_SERVICE_URL:-http://localhost:8080}
export SYSTEM_FRONTEND_PORT=${SYSTEM_FRONTEND_PORT:-8090}
export MANAGER_FRONTEND_PORT=${MANAGER_FRONTEND_PORT:-8091}
export META_FRONTEND_PORT=${META_FRONTEND_PORT:-8092}
export PORTAL_FRONTEND_PORT=${PORTAL_FRONTEND_PORT:-5172}

declare -a PIDS=()
declare -a NAMES=()

cleanup() {
  echo "\nStopping development services..."
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  wait || true
  echo "All services stopped."
}

trap cleanup INT TERM

run_service() {
  local name="$1"
  local dir="$2"
  shift 2
  local cmd=("$@")

  echo "Starting ${name}..."
  (
    cd "${ROOT_DIR}/${dir}" && \
    "${cmd[@]}"
  ) &
  local pid=$!
  PIDS+=("$pid")
  NAMES+=("${name}")
  echo "  ${name} (pid ${pid})"
}

ensure_node_modules() {
  local dir="$1"
  if [[ ! -d "${ROOT_DIR}/${dir}/node_modules" ]]; then
    echo "Installing dependencies for ${dir}..."
    (cd "${ROOT_DIR}/${dir}" && npm install >/dev/null 2>&1)
  fi
}

run_service "system-backend" "system/backend" go run cmd/server/main.go
run_service "manager-backend" "manager/backend" go run cmd/server/main.go
run_service "meta-backend" "meta/backend" go run cmd/server/main.go
run_service "gateway" "gateway" go run cmd/gateway/main.go

ensure_node_modules "system/frontend"
ensure_node_modules "manager/frontend"
ensure_node_modules "meta/frontend"
ensure_node_modules "portal/frontend"

run_service "system-frontend" "system/frontend" npm run dev -- --host 0.0.0.0 --port "${SYSTEM_FRONTEND_PORT}"
run_service "manager-frontend" "manager/frontend" npm run dev -- --host 0.0.0.0 --port "${MANAGER_FRONTEND_PORT}"
run_service "meta-frontend" "meta/frontend" npm run dev -- --host 0.0.0.0 --port "${META_FRONTEND_PORT}"
run_service "portal-frontend" "portal/frontend" npm run dev -- --host 0.0.0.0 --port "${PORTAL_FRONTEND_PORT}"

echo "\nAll services launched. Press Ctrl+C to stop."

while true; do
  if ! wait -n; then
    break
  fi
done

cleanup
