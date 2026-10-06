#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=prometheus,metrics-source,node-exporter
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.monitor-metrics-t2.yml
# ADDP_T2_INPUT_FILES=scripts/infra/node-metrics.py scripts/infra/node-metrics.yml scripts/infra/node-metrics-linux.yml scripts/infra/node-metrics-web.yml scripts/infra/metrics.yml scripts/infra/prometheus.yml scripts/infra/generate-metrics-config.py scripts/infra/prometheus-web.yml scripts/infra/prometheus-http.yml scripts/infra/up.sh scripts/infra/down.sh scripts/infra/ports.sh scripts/infra/status.sh scripts/utils/observability-env.sh scripts/dev/start.sh scripts/prod/start.sh scripts/prod/metrics-platform.yml scripts/prod/metrics-query.yml monitor/backend/internal/resourcequery/ monitor/backend/internal/config/metrics.go monitor/backend/internal/config/config.go scripts/prod/wait-infra.sh scripts/test/monitor-metrics-probe.py scripts/test/metrics-deployment-config_test.py scripts/test/infra-runtime-log-lifecycle_test.py docker-compose.infra.yml docker-compose.yml .env.example
# Own disposable Compose startup, certificates, source files and zero-residue cleanup.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-metrics-t2.XXXXXX")
COMPOSE_PROJECT="addp-metrics-t2-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.monitor-metrics-t2.yml"
export METRICS_T2_WORK="$WORK_DIR" ADDP_METRICS_TLS_DIR="$WORK_DIR/tls"
export ADDP_METRICS_DEPLOYMENT_DIR="$WORK_DIR/deployment"
export ADDP_NODE_METRICS_TLS_DIR="$WORK_DIR/node-tls"
export ADDP_NODE_METRICS_LISTEN=:9100 ADDP_NODE_METRICS_ROOTFS=/ ADDP_NODE_METRICS_PROCFS=/proc ADDP_NODE_METRICS_SYSFS=/sys ADDP_NODE_METRICS_OWNER=t2
export PROMETHEUS_SYSTEM_URL=https://metrics-source:9444 PROMETHEUS_MONITOR_URL=https://metrics-source:9444
export PROMETHEUS_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
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
mkdir -m 755 "$WORK_DIR/tls" "$WORK_DIR/source" "$WORK_DIR/deployment" "$WORK_DIR/node-tls"
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
# The query private key is outside the TLS directory mounted into the center.
mkdir -m 700 "$WORK_DIR/query-tls"
openssl req -new -newkey rsa:2048 -nodes -subj /CN=addp-monitor-query -keyout "$WORK_DIR/query-tls/client.key" -out "$WORK_DIR/query.csr" >/dev/null 2>&1
printf 'extendedKeyUsage=clientAuth\n' >"$WORK_DIR/query-extensions"
openssl x509 -req -in "$WORK_DIR/query.csr" -CA "$ADDP_METRICS_TLS_DIR/ca.crt" -CAkey "$WORK_DIR/ca.key" -CAserial "$ADDP_METRICS_TLS_DIR/ca.srl" -days 1 -extfile "$WORK_DIR/query-extensions" -out "$WORK_DIR/query-tls/client.crt" >/dev/null 2>&1
chmod 600 "$WORK_DIR/query-tls/client.key"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=Untrusted -keyout "$WORK_DIR/untrusted.key" -out "$WORK_DIR/untrusted.crt" >/dev/null 2>&1
cp "$ADDP_METRICS_TLS_DIR/ca.crt" "$ADDP_METRICS_DEPLOYMENT_DIR/control-ca.crt"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=ADDP-Source-T2-CA -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign -keyout "$WORK_DIR/source-ca.key" -out "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" >/dev/null 2>&1
for identity in source-server collector admission; do
  openssl req -new -newkey rsa:2048 -nodes -subj "/CN=$identity" -keyout "$WORK_DIR/$identity.key" -out "$WORK_DIR/$identity.csr" >/dev/null 2>&1
  if [[ "$identity" != source-server ]]; then
    printf 'extendedKeyUsage=clientAuth\n' >"$WORK_DIR/source-extensions"
  else
    printf 'subjectAltName=DNS:localhost,DNS:metrics-source\nextendedKeyUsage=serverAuth\n' >"$WORK_DIR/source-extensions"
  fi
  openssl x509 -req -in "$WORK_DIR/$identity.csr" -CA "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" -CAkey "$WORK_DIR/source-ca.key" -CAserial "$WORK_DIR/source-ca.srl" -CAcreateserial -days 1 -extfile "$WORK_DIR/source-extensions" -out "$WORK_DIR/$identity.crt" >/dev/null 2>&1
done
cp "$WORK_DIR/collector.crt" "$WORK_DIR/collector.key" "$ADDP_METRICS_DEPLOYMENT_DIR/"
cp "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" "$WORK_DIR/source/source-ca.crt"
cp "$WORK_DIR/source-server.crt" "$WORK_DIR/source/server.crt"
cp "$WORK_DIR/source-server.key" "$WORK_DIR/source/server.key"
cp "$WORK_DIR/source-server.crt" "$WORK_DIR/source-server.key" "$WORK_DIR/node-tls/"
mv "$WORK_DIR/node-tls/source-server.crt" "$WORK_DIR/node-tls/server.crt"
mv "$WORK_DIR/node-tls/source-server.key" "$WORK_DIR/node-tls/server.key"
cp "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" "$WORK_DIR/node-tls/ca.crt"
chmod 644 "$WORK_DIR/node-tls/server.key"
chmod 644 "$ADDP_METRICS_DEPLOYMENT_DIR/collector.key" "$WORK_DIR/source/server.key"
printf '%s' "$PROMETHEUS_SERVICE_CLIENT_SECRET" >"$ADDP_METRICS_DEPLOYMENT_DIR/prometheus-client-secret"
# Only one-run fixture secrets; Docker can read them without production permission changes.
chmod 644 "$ADDP_METRICS_DEPLOYMENT_DIR/prometheus-client-secret"
touch "$WORK_DIR/source/requests.log"
chmod 666 "$WORK_DIR/source/requests.log"
python3 "$ROOT_DIR/scripts/infra/generate-metrics-config.py"
cat >"$WORK_DIR/nginx.conf" <<'CONFIG'
worker_processes 1;
pid /tmp/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 128; }
http {
  log_format safe '$request_method $uri $status';
  access_log /source/requests.log safe;
  client_body_temp_path /tmp/client;
  proxy_temp_path /tmp/proxy;
  fastcgi_temp_path /tmp/fastcgi;
  uwsgi_temp_path /tmp/uwsgi;
  scgi_temp_path /tmp/scgi;
  server {
    listen 9443 ssl;
    ssl_certificate /source/server.crt;
    ssl_certificate_key /source/server.key;
    ssl_client_certificate /source/source-ca.crt;
    ssl_verify_client on;
    ssl_protocols TLSv1.2 TLSv1.3;
    location = /metrics { alias /source/metrics; default_type text/plain; }
    location / { return 404; }
  }
  # Controlled protocol fixture, not a System identity issuer or Monitor implementation.
  server {
    listen 9444 ssl;
    ssl_certificate /tls/server.crt;
    ssl_certificate_key /tls/server.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    location = /api/v1/system/oauth/token {
      if ($request_method != POST) { return 405; }
      if ($http_authorization != "@@BASIC@@") { return 401; }
      default_type application/json;
      return 200 '{"access_token":"addp_at_t2_native_oauth","token_type":"Bearer","expires_in":20,"scope":"addp.api"}';
    }
    location = /api/v1/monitor/platform/metrics_discovery {
      if ($http_authorization != "Bearer addp_at_t2_native_oauth") { return 401; }
      if (-f /source/discovery-failed) { return 503; }
      if (-f /source/discovery-redirect) { return 302 https://metrics-source:9444/redirect-target; }
      alias /source/discovery.json;
      default_type application/json;
      add_header Cache-Control no-store;
    }
    location = /redirect-target { default_type application/json; return 200 '[]'; }
    location / { return 404; }
  }
  server { listen 8080; location = /alive { return 200 "fixture alive\n"; } }
}
CONFIG
python3 - "$WORK_DIR/nginx.conf" <<'CONFIG'
from pathlib import Path
import base64, os, sys
p=Path(sys.argv[1])
basic=base64.b64encode(('addp-prometheus:'+os.environ['PROMETHEUS_SERVICE_CLIENT_SECRET']).encode()).decode()
p.write_text(p.read_text().replace('@@BASIC@@','Basic '+basic))
CONFIG
printf 'addp_fixture_resource_bytes{job="spoof",addp_node_id="spoof",addp_tenant_id="spoof",addp_source="spoof",exported_addp_node_id="spoof",addp_target_version="spoof"} 4096\n' >"$WORK_DIR/source/metrics"
echo '[]' >"$WORK_DIR/source/discovery.json"
echo 'Metrics T2: validate fixed-version configuration and mutual TLS'
compose run --rm --no-deps --entrypoint /bin/promtool prometheus check config /etc/prometheus/addp.yml
compose run --rm --no-deps --entrypoint /bin/promtool prometheus check web-config /etc/prometheus/web.yml
compose up -d --wait --wait-timeout 90 metrics-source node-exporter
node_ip=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$(compose ps -q node-exporter)")
printf 'subjectAltName=DNS:localhost,IP:%s\nextendedKeyUsage=serverAuth\n' "$node_ip" >"$WORK_DIR/node-extensions"
openssl x509 -req -in "$WORK_DIR/source-server.csr" -CA "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" -CAkey "$WORK_DIR/source-ca.key" -CAserial "$WORK_DIR/source-ca.srl" -days 1 -extfile "$WORK_DIR/node-extensions" -out "$WORK_DIR/node-tls/server.crt" >/dev/null 2>&1
# Use the owned container's current IP, as the Monitor projection does; no static subnet.
source_ip=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$(compose ps -q metrics-source)")
python3 - "$source_ip" "$WORK_DIR/source/discovery.json" "$WORK_DIR/source-extensions" <<'CONFIG'
from pathlib import Path
import ipaddress, json, sys
address=str(ipaddress.IPv4Address(sys.argv[1]))
Path(sys.argv[2]).write_text(json.dumps([{'targets':[address+':9443'],'labels':{
 'addp_node_id':'11111111-1111-4111-8111-111111111111','addp_monitor_kind':'host_resources',
 'addp_source':'node_exporter','__meta_addp_target_version':'1'}}]))
Path(sys.argv[3]).write_text('subjectAltName=DNS:localhost,IP:'+address+'\nextendedKeyUsage=serverAuth\n')
CONFIG
openssl x509 -req -in "$WORK_DIR/source-server.csr" -CA "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" -CAkey "$WORK_DIR/source-ca.key" -CAserial "$WORK_DIR/source-ca.srl" -CAcreateserial -days 1 -extfile "$WORK_DIR/source-extensions" -out "$WORK_DIR/source/server.crt" >/dev/null 2>&1
compose exec -T metrics-source nginx -s reload
compose up -d --wait --wait-timeout 90 prometheus metrics-source
export METRICS_T2_PROJECT="$COMPOSE_PROJECT" METRICS_T2_COMPOSE="$COMPOSE_FILE"
python3 "$ROOT_DIR/scripts/test/monitor-metrics-probe.py"
