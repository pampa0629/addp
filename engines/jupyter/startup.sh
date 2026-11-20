#!/bin/bash
set -eu
nginx -c /app/nginx.conf -g 'daemon off;' &
proxy=$!
cd /app
gunicorn --bind unix:/tmp/notebook-api.sock --workers 1 --worker-class gthread \
    --threads "${JUPYTER_GUNICORN_THREADS:-8}" --timeout "${JUPYTER_GUNICORN_TIMEOUT:-7200}" 'api_server:runtime_app()' &
api=$!
shutdown() {
    kill -TERM "$api" "$proxy" 2>/dev/null || true
    wait "$api" "$proxy" 2>/dev/null || true
}
trap shutdown EXIT
trap 'exit 0' TERM INT
wait -n "$proxy" "$api"
