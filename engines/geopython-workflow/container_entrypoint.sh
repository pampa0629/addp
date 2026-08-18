#!/bin/sh
set -eu

gunicorn --bind 0.0.0.0:8099 \
  --workers 4 \
  --timeout "${GEOPYTHON_WORKFLOW_GUNICORN_TIMEOUT:-7200}" \
  api_server:app &
server_pid=$!

cleanup() {
  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
}
trap cleanup INT TERM

ready=0
attempt=0
while [ "$attempt" -lt 60 ]; do
  if python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8099/health', timeout=3)" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$server_pid" 2>/dev/null; then
    wait "$server_pid"
    exit $?
  fi
  attempt=$((attempt + 1))
  sleep 1
done

if [ "$ready" -eq 1 ]; then
  python -c "from api_server import register_to_system_with_retry; register_to_system_with_retry()" || true
else
  echo "GeoPython Workflow health check timed out; skip System registration" >&2
fi

wait "$server_pid"
