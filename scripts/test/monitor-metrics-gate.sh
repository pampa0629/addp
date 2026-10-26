#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=prometheus,metrics-source,node-exporter,metrics-control,postgres,postgres-metrics,postgres-metrics-web,python-http-agent,python-http-copilot
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.monitor-metrics-t2.yml
# ADDP_T2_INPUT_FILES=common-python/ agent/backend/main.py copilot/backend/main.py scripts/test/python-http-metrics-source.py scripts/test/python-http-metrics-probe.py scripts/test/metrics-http-budget.py scripts/test/postgres-metrics-lifecycle_test.py scripts/test/postgres-lifecycle-probe.py business/scripts/stop.sh business/docker-compose.yml business/scripts/start.sh business/.env.example scripts/infra/postgres-metrics.yml scripts/infra/postgres-observation/ scripts/test/postgres-observation-probe.py scripts/utils/deployment-inventory.py scripts/utils/container-proof.py scripts/infra/metrics-control.yml scripts/infra/metrics-control.conf scripts/infra/node-metrics.py scripts/infra/node-metrics.yml scripts/infra/node-metrics-linux.yml scripts/infra/node-metrics-desktop.yml scripts/infra/node-metrics-web.yml scripts/infra/metrics.yml scripts/infra/prometheus.yml scripts/infra/generate-metrics-config.py scripts/infra/prometheus-web.yml scripts/infra/prometheus-http.yml scripts/infra/up.sh scripts/infra/down.sh scripts/infra/ports.sh scripts/infra/status.sh scripts/utils/observability-env.sh scripts/dev/start.sh scripts/dev/stop.sh scripts/dev/lifecycle-lock.sh scripts/dev/process-metrics-launch.py scripts/dev/worker-units.sh scripts/dev/ports.sh scripts/test/dev-process-metrics-plan_test.py scripts/prod/start.sh scripts/prod/stop.sh scripts/prod/generate-process-metrics-platform.py scripts/prod/metrics-platform.yml scripts/prod/metrics-query.yml common/processmetrics/ common/httpmetrics/ common/modulelifecycle/ common/config/process_metrics.go common/models/process_metrics.go common/models/container_observation.go common/client/system_service.go common/client/system_runtime_instances.go common/client/system_observability_identity.go common/models/module_runtime_query.go common/models/observability_identity.go common/models/service_expectations.go common/models/postgresql_metrics.go common/client/system_engine_deployments.go monitor/backend/internal/metricsdiscovery/ monitor/backend/internal/resourcequery/ monitor/backend/internal/config/metrics.go monitor/backend/internal/config/config.go common/client/system_host_node.go common/client/system_host_node_scope.go scripts/prod/wait-infra.sh scripts/test/monitor-metrics-probe.py scripts/test/metrics-deployment-config_test.py scripts/test/infra-runtime-log-lifecycle_test.py docker-compose.infra.yml docker-compose.yml .env.example monitor/backend/internal/service/ monitor/backend/internal/models/ monitor/backend/internal/repository/
# Own disposable Compose startup, certificates, source files and zero-residue cleanup.
set -euo pipefail
[[ $# -le 1 ]] || { echo 'Metrics T2: unexpected arguments' >&2; exit 2; }
case "${1:-}" in
  ''|--case=all) metrics_case=all ;;
  --case=postgres-observation) metrics_case=postgres-observation ;;
  --case=postgres-source) metrics_case=postgres-source ;;
  --case=http-quality) metrics_case=http-quality ;;
  --case=mtls-preflight) metrics_case=mtls-preflight ;;
  --case=process-alerts) metrics_case=process-alerts ;;
  --case=host-resources) metrics_case=host-resources ;;
  --case=cgroup-source) metrics_case=cgroup-source ;;
  *) echo 'Metrics T2: unsupported case' >&2; exit 2 ;;
esac
export ADDP_METRICS_T2_CASE="$metrics_case"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
python3 -m unittest "$ROOT_DIR/scripts/test/metrics-deployment-config_test.py"
if [[ "$metrics_case" == all || "$metrics_case" == postgres-observation || "$metrics_case" == postgres-source ]]; then
  python3 -m unittest "$ROOT_DIR/scripts/test/postgres-metrics-lifecycle_test.py"
fi
# Test-owned transport starts dynamically, then the probe pins its assigned port.
export ADDP_METRICS_DESKTOP_LOOPBACK_PORTS= ADDP_NODE_METRICS_PUBLISH_PORT=0
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-metrics-t2.XXXXXX")
COMPOSE_PROJECT="addp-metrics-t2-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.monitor-metrics-t2.yml"
export METRICS_T2_WORK="$WORK_DIR" ADDP_METRICS_TLS_DIR="$WORK_DIR/tls"
if [[ "$metrics_case" == http-quality || "$metrics_case" == mtls-preflight ]]; then
  mkdir -m 700 "$WORK_DIR/go-cache" "$WORK_DIR/go-tmp" "$WORK_DIR/tmp"
  export GOCACHE="$WORK_DIR/go-cache" GOTMPDIR="$WORK_DIR/go-tmp" TMPDIR="$WORK_DIR/tmp"
  export GOMAXPROCS=2 GOFLAGS=-p=2
fi
if [[ -n "${ADDP_HTTP_BUDGET_RECEIPT:-}" ]]; then
  python3 - "$ADDP_HTTP_BUDGET_RECEIPT" "$COMPOSE_PROJECT" "$WORK_DIR" <<'RECEIPT'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]);tmp=p.with_suffix('.pending');tmp.write_text(json.dumps({'project':sys.argv[2],'work':sys.argv[3]}));tmp.replace(p)
RECEIPT
fi
export ADDP_POSTGRES_METRICS_SOURCE_DIR="$WORK_DIR/postgres-source"
export ADDP_POSTGRES_METRICS_WEB_DIR="$WORK_DIR/postgres-web"
export ADDP_POSTGRES_METRICS_USER="$(id -u):$(id -g)"
if [[ "$(id -u)" == 0 ]]; then export ADDP_POSTGRES_METRICS_USER=65534:65534; fi
export METRICS_T2_POSTGRES_SOURCE_ENABLED=false
export METRICS_T2_DIRECTORY_AUTH=false METRICS_T2_HBA_FILE=/var/lib/postgresql/data/pg_hba.conf
export ADDP_METRICS_DEPLOYMENT_DIR="$WORK_DIR/deployment"
export ADDP_NODE_METRICS_TLS_DIR="$WORK_DIR/node-tls"
export ADDP_NODE_METRICS_LISTEN=:9100 ADDP_NODE_METRICS_ROOTFS=/ ADDP_NODE_METRICS_PROCFS=/proc ADDP_NODE_METRICS_SYSFS=/sys ADDP_NODE_METRICS_OWNER=t2
export ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX=--collector
export PROMETHEUS_SYSTEM_URL=https://metrics-control:9444 PROMETHEUS_MONITOR_URL=https://metrics-control:9444
export ADDP_METRICS_CONTROL_ENABLED=true ADDP_METRICS_CONTROL_DIR="$WORK_DIR/control"
export ADDP_METRICS_CONTROL_GATEWAY_URL=http://metrics-source:8081
export PROMETHEUS_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export METRICS_T2_POSTGRES_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
compose(){ docker compose --env-file /dev/null -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }
cleanup(){
  local result=$?
  local cleanup_verified=true
  trap - EXIT INT TERM
  set +e
  if [[ -n "${ADDP_HTTP_BUDGET_RECEIPT:-}" && -f "$ADDP_HTTP_BUDGET_RECEIPT" ]]; then
    python3 - "$ADDP_HTTP_BUDGET_RECEIPT" <<'CLEANUP_RECEIPT'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]);value=json.loads(p.read_text());value['cleanup_started']=True
proof=Path(value['work'])/'source-tls-proof.json'
if proof.exists():value['source_tls_proof']=json.loads(proof.read_text())
publication=Path(value['work'])/'source-tls-publication.json'
if publication.exists():value['source_tls_publication']=json.loads(publication.read_text())
pending=p.with_suffix('.pending');pending.write_text(json.dumps(value));pending.replace(p)
CLEANUP_RECEIPT
  fi
  # The new SQL source's logs are checked for secret exposure in memory by its
  # probe. Never dump those raw logs on failure before that check succeeds.
  if [[ "$result" != 0 ]]; then compose logs --no-color --tail=60 prometheus metrics-source node-exporter metrics-control postgres >&2; fi
  compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || { result=1; cleanup_verified=false; }
  # Producers are stopped: phase/counter files can no longer be concurrently written.
  # Capture even an early Go failure, before Python is entered, then destroy files.
  if [[ -n "${ADDP_HTTP_BUDGET_RECEIPT:-}" && -f "$ADDP_HTTP_BUDGET_RECEIPT" ]]; then
    python3 - "$ADDP_HTTP_BUDGET_RECEIPT" "$ROOT_DIR/scripts/test/metrics-http-budget.py" <<'REQUEST_TRAFFIC'
import json,runpy,sys
from pathlib import Path
p=Path(sys.argv[1]);value=json.loads(p.read_text());failed=False
source=Path(value['work'])/'source'
try:
    value.update(runpy.run_path(sys.argv[2])['request_traffic'](source))
except (OSError,ValueError,RuntimeError,UnicodeError):
    value['request_traffic_error']='invalid_or_missing_phase_receipt';failed=True
try:
    probe=Path(sys.argv[2]).with_name('python-http-metrics-probe.py')
    value['python_sources']=runpy.run_path(str(probe))['python_evidence'](source)
except (OSError,ValueError,RuntimeError,UnicodeError):
    value['python_sources']={'status':'unavailable'};failed=True
pending=p.with_suffix('.pending');pending.write_text(json.dumps(value));pending.replace(p)
raise SystemExit(1 if failed else 0)
REQUEST_TRAFFIC
    [[ "$?" == 0 ]] || result=1
  fi
  local resource remaining
  for resource in container network volume; do
    case "$resource" in
      container) remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || { result=1; cleanup_verified=false; } ;;
      network) remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || { result=1; cleanup_verified=false; } ;;
      volume) remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || { result=1; cleanup_verified=false; } ;;
    esac
    [[ -z "$remaining" ]] || { result=1; cleanup_verified=false; }
  done
  if [[ "$result" != 0 ]]; then cat "$WORK_DIR/cleanup.log" >&2; fi
  rm -rf "$WORK_DIR" || { result=1; cleanup_verified=false; }
  [[ ! -e "$WORK_DIR" ]] || { result=1; cleanup_verified=false; }
  if [[ -n "${ADDP_HTTP_BUDGET_RECEIPT:-}" && -f "$ADDP_HTTP_BUDGET_RECEIPT" ]]; then
    python3 - "$ADDP_HTTP_BUDGET_RECEIPT" "$cleanup_verified" <<'CLEANUP_VERIFIED'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]);value=json.loads(p.read_text());value['cleanup_verified']=sys.argv[2]=='true'
pending=p.with_suffix('.pending');pending.write_text(json.dumps(value));pending.replace(p)
CLEANUP_VERIFIED
  fi
  if [[ "$result" == 0 ]]; then echo 'Metrics T2: zero owned containers/networks/volumes and temporary files'; fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# The private work directory owns this mandatory startup secret. Rendering only
# the shared source transport (Hosted T4) does not need a database credential.
printf '%s' "$METRICS_T2_POSTGRES_PASSWORD" >"$WORK_DIR/postgres-password"
chmod 644 "$WORK_DIR/postgres-password"
export METRICS_T2_PROJECT="$COMPOSE_PROJECT" METRICS_T2_COMPOSE="$COMPOSE_FILE"
python3 - "$ROOT_DIR" "$WORK_DIR" <<'PGAUTH'
import importlib.util
from pathlib import Path
import sys
spec = importlib.util.spec_from_file_location('owned_auth_directory', Path(sys.argv[1]) / 'scripts/infra/postgres-observation/authentication.py')
authentication = importlib.util.module_from_spec(spec)
spec.loader.exec_module(authentication)
authentication.prepare_business(Path(sys.argv[2]).resolve() / 'postgres-auth',
                                'local all all trust\nhost all all 0.0.0.0/0 scram-sha-256\nhost all all ::0/0 scram-sha-256\n')
PGAUTH
postgres_directory_probe(){
  # Recreate only this gate's disposable tmpfs database. Never use shared PG.
  compose stop postgres-metrics-web postgres-metrics
  compose rm -sf postgres
  export METRICS_T2_DIRECTORY_AUTH=true METRICS_T2_HBA_FILE=/etc/addp-postgres-auth/pg_hba.conf
  compose up -d --wait --wait-timeout 90 postgres
  python3 "$ROOT_DIR/scripts/test/postgres-observation-probe.py"
  if [[ "$METRICS_T2_POSTGRES_SOURCE_ENABLED" == true ]]; then python3 "$ROOT_DIR/scripts/test/postgres-lifecycle-probe.py"; fi
}
if [[ "$metrics_case" == postgres-observation ]]; then
  compose up -d --wait --wait-timeout 90 postgres
  python3 "$ROOT_DIR/scripts/test/postgres-observation-probe.py"
  postgres_directory_probe
  exit 0
fi
mkdir -m 755 "$WORK_DIR/tls" "$WORK_DIR/source" "$WORK_DIR/deployment" "$WORK_DIR/node-tls" "$WORK_DIR/control" "$WORK_DIR/query-tests"
# A one-run CA and deployer client; never install these in a personal environment.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=ADDP-Metrics-T2-CA -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign -keyout "$WORK_DIR/ca.key" -out "$ADDP_METRICS_TLS_DIR/ca.crt" >/dev/null 2>&1
for identity in server health; do
  openssl req -new -newkey rsa:2048 -nodes -subj "/CN=$identity" -keyout "$ADDP_METRICS_TLS_DIR/$identity.key" -out "$WORK_DIR/$identity.csr" >/dev/null 2>&1
  if [[ "$identity" == server ]]; then
    printf 'subjectAltName=DNS:localhost,DNS:prometheus,DNS:metrics-source,DNS:metrics-control\nextendedKeyUsage=serverAuth\n' >"$WORK_DIR/extensions"
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
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=ADDP-Source-T2-CA -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign -keyout "$WORK_DIR/source-ca.key" -out "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" >/dev/null 2>&1
for identity in source-server collector admission; do
  openssl req -new -newkey rsa:2048 -nodes -subj "/CN=$identity" -keyout "$WORK_DIR/$identity.key" -out "$WORK_DIR/$identity.csr" >/dev/null 2>&1
  if [[ "$identity" != source-server ]]; then
    printf 'extendedKeyUsage=clientAuth\n' >"$WORK_DIR/source-extensions"
  else
    printf 'subjectAltName=DNS:localhost,DNS:metrics-source,DNS:postgres-metrics,DNS:host.docker.internal,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' >"$WORK_DIR/source-extensions"
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
# Control-plane trust is separate from center query/health and node source trust.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=ADDP-Control-T2-CA -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign -keyout "$WORK_DIR/control-ca.key" -out "$ADDP_METRICS_DEPLOYMENT_DIR/control-ca.crt" >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -nodes -subj /CN=metrics-control -keyout "$ADDP_METRICS_CONTROL_DIR/server.key" -out "$WORK_DIR/control.csr" >/dev/null 2>&1
printf 'subjectAltName=DNS:localhost,DNS:metrics-control\nextendedKeyUsage=serverAuth\n' >"$WORK_DIR/control-extensions"
openssl x509 -req -in "$WORK_DIR/control.csr" -CA "$ADDP_METRICS_DEPLOYMENT_DIR/control-ca.crt" -CAkey "$WORK_DIR/control-ca.key" -set_serial 1 -days 1 -extfile "$WORK_DIR/control-extensions" -out "$ADDP_METRICS_CONTROL_DIR/server.crt" >/dev/null 2>&1
chmod 644 "$ADDP_METRICS_CONTROL_DIR/server.key"
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
    ssl_certificate /tmp/addp-source-tls/server.crt;
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
    listen 8081;
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
config=p.read_text().replace('@@BASIC@@','Basic '+basic)
# Internal-only endpoints share the owned source's mTLS identity and lifecycle.
# These are controlled metric inputs, not six real managed hosts.
servers=[]
for index in range(6):
    servers.append('''  server {
    listen %d ssl;
    ssl_certificate /tmp/addp-source-tls/server.crt;
    ssl_certificate_key /source/server.key;
    ssl_client_certificate /source/source-ca.crt;
    ssl_verify_client on;
    ssl_protocols TLSv1.2 TLSv1.3;
    location = /metrics { alias /source/overview-%d.prom; default_type text/plain; }
    location / { return 404; }
  }
''' % (19450+index, index))
assert config.endswith('}\n')
p.write_text(config[:-2]+''.join(servers)+'}\n')
CONFIG
printf 'addp_fixture_resource_bytes{job="spoof",addp_node_id="spoof",addp_tenant_id="spoof",addp_source="spoof",exported_addp_node_id="spoof",addp_target_version="spoof"} 4096\n' >"$WORK_DIR/source/metrics"
echo '[]' >"$WORK_DIR/source/discovery.json"
echo 'Metrics T2: validate fixed-version configuration and mutual TLS'
compose up -d --wait --wait-timeout 90 metrics-source node-exporter postgres
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
openssl x509 -req -in "$WORK_DIR/source-server.csr" -CA "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" -CAkey "$WORK_DIR/source-ca.key" -CAserial "$WORK_DIR/source-ca.srl" -CAcreateserial -days 1 -extfile "$WORK_DIR/source-extensions" -out "$WORK_DIR/source-server-final.crt" >/dev/null 2>&1
publication_state=$(compose exec -T metrics-source sh -ec '
tls_dir=$1
stat -c "%d:%i %Y" "$tls_dir/server.crt"
umask 077
cat > "$tls_dir/server.crt.next"
mv "$tls_dir/server.crt.next" "$tls_dir/server.crt"
stat -c "%d:%i %Y" "$tls_dir/server.crt"
' _ /tmp/addp-source-tls < "$WORK_DIR/source-server-final.crt")
python3 - "$WORK_DIR/source/server.crt" "$WORK_DIR/source-server-final.crt" "$WORK_DIR/source-tls-publication.json" "$publication_state" <<'CERTIFICATE'
from pathlib import Path
import hashlib,json,re,ssl,sys
digest=lambda path: hashlib.sha256(ssl.PEM_cert_to_DER_cert(path.read_text())).hexdigest()
rows=[line.split() for line in sys.argv[4].splitlines()]
assert len(rows)==2 and all(len(row)==2 and re.fullmatch('[0-9]+:[0-9]+',row[0]) and re.fullmatch('[0-9]+',row[1]) for row in rows), 'source_certificate_publication_metadata_invalid'
prior=digest(Path(sys.argv[1]))
proof={'atomic_identity_changed':rows[0][0]!=rows[1][0],
       'same_second_mtime':rows[0][1]==rows[1][1],
       'prior_leaf_sha256':prior,'expected_leaf_sha256':digest(Path(sys.argv[2]))}
assert proof['atomic_identity_changed'] and prior!=proof['expected_leaf_sha256'], 'source_certificate_publication_failed'
output=Path(sys.argv[3]);temporary=output.with_suffix('.pending')
temporary.write_text(json.dumps(proof));temporary.replace(output)
CERTIFICATE
# Refresh only the next-start bootstrap. Nginx's live certificate path is tmpfs.
cp "$WORK_DIR/source-server-final.crt" "$WORK_DIR/source/server.crt"
compose exec -T metrics-source nginx -t
compose exec -T metrics-source nginx -s reload
compose up -d --wait --wait-timeout 90 prometheus metrics-source metrics-control
compose exec -T prometheus /bin/promtool check config /etc/prometheus/addp.yml
compose exec -T prometheus /bin/promtool check web-config /etc/prometheus/web.yml
compose exec -T metrics-control nginx -t
export METRICS_T2_PROJECT="$COMPOSE_PROJECT" METRICS_T2_COMPOSE="$COMPOSE_FILE"
if [[ "$metrics_case" == all || "$metrics_case" == postgres-source ]]; then
  export METRICS_T2_POSTGRES_SOURCE_ENABLED=true
  python3 "$ROOT_DIR/scripts/test/postgres-observation-probe.py"
  python3 "$ROOT_DIR/scripts/test/postgres-lifecycle-probe.py"
  postgres_directory_probe
fi
if [[ "$metrics_case" == postgres-source ]]; then exit 0; fi
if [[ "$metrics_case" != host-resources && "$metrics_case" != mtls-preflight ]]; then
# Compile the current Common test source for the owned Linux container, without
# installing Go or mounting the workspace into it. The cleanup trap owns it.
process_arch=$(docker info --format '{{.Architecture}}')
case "$process_arch" in aarch64|arm64) process_arch=arm64 ;; x86_64|amd64) process_arch=amd64 ;; *) echo 'Unsupported metrics T2 architecture' >&2; exit 1 ;; esac
(cd "$ROOT_DIR/common" && CGO_ENABLED=0 GOOS=linux GOARCH="$process_arch" GOWORK=off go test -c ./processmetrics -o "$WORK_DIR/source/process-source.test")
mkdir -m 755 "$WORK_DIR/source/process-tls"
# Native admission and SD use the inspected IP; Desktop rewrites only its
# explicitly published loopback transport. The source validates its declaration.
printf 'subjectAltName=DNS:localhost,DNS:metrics-source,DNS:host.docker.internal,IP:127.0.0.1,IP:%s\nextendedKeyUsage=serverAuth\n' "$source_ip" >"$WORK_DIR/process-extensions"
openssl x509 -req -in "$WORK_DIR/source-server.csr" -CA "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" -CAkey "$WORK_DIR/source-ca.key" -CAserial "$WORK_DIR/source-ca.srl" -days 1 -extfile "$WORK_DIR/process-extensions" -out "$WORK_DIR/source/process-tls/server.crt" >/dev/null 2>&1
cp "$WORK_DIR/source-server.key" "$WORK_DIR/source/process-tls/server.key"
cp "$ADDP_METRICS_DEPLOYMENT_DIR/source-ca.crt" "$WORK_DIR/source/process-tls/ca.crt"
chmod 644 "$WORK_DIR/source/process-tls/server.key"
fi
python3 "$ROOT_DIR/scripts/test/monitor-metrics-probe.py"
