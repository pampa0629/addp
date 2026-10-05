#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=prometheus,metrics-source
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.monitor-metrics-t2.yml
# ADDP_T2_INPUT_FILES=scripts/infra/metrics.yml scripts/infra/prometheus.yml scripts/infra/prometheus-web.yml scripts/infra/prometheus-http.yml scripts/infra/up.sh scripts/infra/down.sh scripts/infra/ports.sh scripts/infra/status.sh scripts/utils/observability-env.sh scripts/dev/start.sh scripts/prod/start.sh scripts/prod/wait-infra.sh scripts/test/monitor-metrics-probe.py scripts/test/infra-runtime-log-lifecycle_test.py docker-compose.infra.yml .env.example
# Own disposable Compose startup, certificates, source files and zero-residue cleanup.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-metrics-t2.XXXXXX")
COMPOSE_PROJECT="addp-metrics-t2-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.monitor-metrics-t2.yml"
export METRICS_T2_WORK="$WORK_DIR" ADDP_METRICS_TLS_DIR="$WORK_DIR/tls"
compose(){ docker compose --env-file /dev/null -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }
cleanup(){
  local result=$?
  trap - EXIT INT TERM
  set +e
  if [[ "$result" != 0 ]]; then compose logs --no-color --tail=60 >&2; fi
  compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || result=1
  local resource remaining
  for resource in container network volume; do
    case "$resource" in
      container) remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || result=1 ;;
      network) remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || result=1 ;;
      volume) remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || result=1 ;;
    esac
    [[ -z "$remaining" ]] || result=1
  done
  if [[ "$result" != 0 ]]; then cat "$WORK_DIR/cleanup.log" >&2; fi
  rm -rf "$WORK_DIR" || result=1
  [[ ! -e "$WORK_DIR" ]] || result=1
  if [[ "$result" == 0 ]]; then echo 'Metrics T2: zero owned containers/networks/volumes and temporary files'; fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -m 755 "$WORK_DIR/tls" "$WORK_DIR/source"
# A one-run CA and deployer client; never install these in a personal environment.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=ADDP-Metrics-T2-CA -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign -keyout "$WORK_DIR/ca.key" -out "$ADDP_METRICS_TLS_DIR/ca.crt" >/dev/null 2>&1
for identity in server health; do
  openssl req -new -newkey rsa:2048 -nodes -subj "/CN=$identity" -keyout "$ADDP_METRICS_TLS_DIR/$identity.key" -out "$WORK_DIR/$identity.csr" >/dev/null 2>&1
  if [[ "$identity" == server ]]; then
    printf 'subjectAltName=DNS:localhost,DNS:prometheus,DNS:metrics-source\nextendedKeyUsage=serverAuth\n' >"$WORK_DIR/extensions"
  else
    printf 'extendedKeyUsage=clientAuth\n' >"$WORK_DIR/extensions"
  fi
  openssl x509 -req -in "$WORK_DIR/$identity.csr" -CA "$ADDP_METRICS_TLS_DIR/ca.crt" -CAkey "$WORK_DIR/ca.key" -CAcreateserial -days 1 -extfile "$WORK_DIR/extensions" -out "$ADDP_METRICS_TLS_DIR/$identity.crt" >/dev/null 2>&1
  # Ephemeral fixture files only; the parent directory remains owner-private.
  chmod 644 "$ADDP_METRICS_TLS_DIR/$identity.key"
done
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=Untrusted -keyout "$WORK_DIR/untrusted.key" -out "$WORK_DIR/untrusted.crt" >/dev/null 2>&1
python3 - "$ROOT_DIR/scripts/infra/prometheus.yml" "$WORK_DIR/prometheus.yml" <<'CONFIG'
from pathlib import Path
import sys
base = Path(sys.argv[1]).read_text()
job = base[base.index('  - job_name: prometheus'):]
# Reuse the production scrape budget; only the fixture endpoint differs.
fixture = job.replace('job_name: prometheus', 'job_name: fixture').replace('[localhost:9090]', '[metrics-source:9443]')
Path(sys.argv[2]).write_text(base + fixture)
CONFIG
cat >"$WORK_DIR/nginx.conf" <<'CONFIG'
worker_processes 1;
pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 128; }
http {
  access_log off;
  client_body_temp_path /tmp/client;
  proxy_temp_path /tmp/proxy;
  fastcgi_temp_path /tmp/fastcgi;
  uwsgi_temp_path /tmp/uwsgi;
  scgi_temp_path /tmp/scgi;
  server {
    listen 9443 ssl;
    ssl_certificate /tls/server.crt;
    ssl_certificate_key /tls/server.key;
    ssl_client_certificate /tls/ca.crt;
    ssl_verify_client on;
    ssl_protocols TLSv1.2 TLSv1.3;
    location = /metrics { alias /source/metrics; default_type text/plain; }
    location / { return 404; }
  }
  server { listen 8080; location = /alive { return 200 "fixture alive\n"; } }
}
CONFIG
printf 'addp_fixture_resource_bytes{job="spoof"} 4096\n' >"$WORK_DIR/source/metrics"
echo 'Metrics T2: validate fixed-version configuration and mutual TLS'
compose run --rm --no-deps --entrypoint /bin/promtool prometheus check config /etc/prometheus/addp.yml
compose run --rm --no-deps --entrypoint /bin/promtool prometheus check web-config /etc/prometheus/web.yml
compose up -d --wait --wait-timeout 90 prometheus metrics-source
export METRICS_T2_PROJECT="$COMPOSE_PROJECT" METRICS_T2_COMPOSE="$COMPOSE_FILE"
python3 "$ROOT_DIR/scripts/test/monitor-metrics-probe.py"
