#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=minio,runtime-log-store-init,loki,runtime-log-api,trace-store-init,tempo,trace-query,alloy,trace-load
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.monitor-traces-t2.yml
# ADDP_T2_INPUT_FILES=common-python/ agent/backend/main.py copilot/backend/main.py scripts/test/trace-python-source.py scripts/test/trace-capacity.py scripts/test/trace-capacity_test.py monitor/backend/internal/service/traces_capacity_test.go common/httptrace/ common/logger/ common/runtimelog/ common/modulelifecycle/ common/middleware/ common/client/ common/models/module_runtime_query.go monitor/backend/internal/tracequery/ monitor/backend/internal/service/traces.go monitor/backend/internal/service/traces_test.go monitor/backend/internal/service/traces_integration_test.go scripts/test/trace-deployment-fixture.py scripts/prod/generate-trace-platform.py scripts/prod/start.sh scripts/test/trace-deployment-config_test.py scripts/infra/up.sh scripts/infra/down.sh scripts/infra/ports.sh scripts/infra/status.sh scripts/utils/observability-env.sh docker-compose.infra.yml .env.example scripts/infra/traces.yml scripts/infra/traces-platform.yml scripts/infra/traces.alloy scripts/infra/tempo.yml scripts/infra/trace-query.conf scripts/infra/init-trace-store.sh scripts/infra/generate-trace-config.py scripts/infra/runtime-logs.yml scripts/infra/runtime-logs.alloy scripts/infra/loki.yml scripts/infra/init-runtime-log-store.sh scripts/infra/runtime-log-api.conf.template scripts/test/docker-compose.system-runtime-log-t2.yml scripts/infra/Dockerfile.minio
# Own disposable Compose startup, real native producers and zero-residue cleanup.
set -euo pipefail
[[ $# == 0 || ( $# == 1 && $1 == --capacity ) ]] || { echo "usage: $0 [--capacity]" >&2; exit 2; }
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-traces-t2.XXXXXX")
COMPOSE_PROJECT="addp-traces-t2-$(python3 -c 'import uuid;print(uuid.uuid4().hex)')"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.monitor-traces-t2.yml"
export LOKI_TEST_SECRET="$(python3 -c 'import secrets;print(secrets.token_hex(32))')"
export TEMPO_S3_SECRET_KEY="$(python3 -c 'import secrets;print(secrets.token_hex(32))')"
export LOKI_READ_TOKEN="$LOKI_TEST_SECRET-read" LOKI_WRITE_TOKEN="$LOKI_TEST_SECRET-write" LOKI_RETENTION_HOURS=168
export ADDP_TRACE_DEPLOYMENT_DIR="$WORK_DIR/deployment" ADDP_RUNTIME_LOG_OWNER="$(id -u):$(id -g)" LOKI_TEST_SOURCE="$WORK_DIR/source"
export TRACE_T2_OTLP_PUBLISH_PORT=0
export TRACE_T2_WORK="$WORK_DIR" TRACE_T2_PROJECT="$COMPOSE_PROJECT" TRACE_T2_COMPOSE="$COMPOSE_FILE"
mkdir -m 700 "$LOKI_TEST_SOURCE"
compose(){ docker compose --env-file /dev/null -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }
cleanup(){
 local result=$? remaining resource owned_load_ids owned_load_id cleanup_ok=true
 echo "Trace T2 cleanup, original status $result"
 trap - EXIT INT TERM
 set +e
 if [[ "$result" != 0 ]]; then compose logs --no-color --tail=20 trace-query tempo trace-store-init alloy >"$WORK_DIR/container.log" 2>&1; fi
 # Compose down omits one-off run containers. Stop only this run's producer
 # before removing its network/volumes, including interrupted startup paths.
 owned_load_ids=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --filter 'label=com.docker.compose.service=trace-load') || { result=1; cleanup_ok=false; }
 while IFS= read -r owned_load_id; do
  [[ -n "$owned_load_id" ]] || continue
  docker container rm -f -v "$owned_load_id" >"$WORK_DIR/load-cleanup.log" 2>&1 || { result=1; cleanup_ok=false; }
 done <<<"$owned_load_ids"
 compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || { result=1; cleanup_ok=false; }
 for resource in container network volume; do
  case "$resource" in
   container) remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || { result=1; cleanup_ok=false; };;
   network) remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || { result=1; cleanup_ok=false; };;
   volume) remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || { result=1; cleanup_ok=false; };;
  esac
  [[ -z "$remaining" ]] || { result=1; cleanup_ok=false; }
 done
 if [[ "$result" != 0 ]]; then
  python3 - "$WORK_DIR" <<'PY'
import os,sys
from pathlib import Path
for name in ['build.log','validate.log','start.log','integration.log','container.log','cleanup.log']:
 p=Path(sys.argv[1])/name
 if p.exists():
  text=p.read_text(errors='replace')[-12000:]
  for key in ['LOKI_TEST_SECRET','TEMPO_S3_SECRET_KEY','LOKI_READ_TOKEN','LOKI_WRITE_TOKEN']: text=text.replace(os.environ[key],'[REDACTED]')
  print(name+':\n'+text)
PY
 fi
 if [[ -f "$WORK_DIR/capacity-report.path" ]]; then
  python3 - "$WORK_DIR/capacity-report.path" "$cleanup_ok" <<'PYREPORT' || result=1
from pathlib import Path
import json,sys
path=Path(Path(sys.argv[1]).read_text());report=json.loads(path.read_text())
report['cleanup']={'zero_owned_containers_networks_volumes':sys.argv[2]=='true'}
path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
PYREPORT
 fi
 rm -rf "$WORK_DIR" || result=1
 [[ ! -e "$WORK_DIR" ]] || result=1
 if [[ "$result" == 0 ]]; then echo 'Trace T2: zero owned containers/networks/volumes and temporary files'; fi
 exit "$result"
}
trap 'echo "Trace gate failed at line $LINENO" >&2' ERR
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$ROOT_DIR/scripts/test/trace-deployment-fixture.py" "$WORK_DIR"
if [[ ${1:-} == --capacity ]]; then
 python3 "$ROOT_DIR/scripts/test/trace-capacity.py" "$WORK_DIR"
 exit $?
fi
# The standard functional chain now crosses the shared Python implementation;
# the separately selected capacity model remains its approved three-Go chain.
export TRACE_T2_PYTHON="${MONITOR_TRACES_PYTHON:-$ROOT_DIR/agent/backend/venv/bin/python}"
export TRACE_T2_PYTHON_FIXTURE="$ROOT_DIR/scripts/test/trace-python-source.py"
export PYTHONPATH="$ROOT_DIR/common-python${PYTHONPATH:+:$PYTHONPATH}"
"$TRACE_T2_PYTHON" -c 'import fastapi,uvicorn,httpx; import addp_common.httptrace.runtime,addp_common.httptrace.server,addp_common.httptrace.client'
echo "Trace T2: validate published configuration and initialize isolated stores"
compose build minio runtime-log-store-init trace-store-init >"$WORK_DIR/build.log" 2>&1 || { echo "Trace T2 build failed"; cat "$WORK_DIR/build.log"; exit 1; }
echo "Trace T2 build passed"
if ! compose run --rm --no-deps alloy validate /trace-deployment/runtime.alloy >"$WORK_DIR/validate.log" 2>&1; then
 echo "Trace T2 config validation failed"; cat "$WORK_DIR/validate.log"; exit 1
fi
if ! compose up -d --wait --wait-timeout 180 alloy >"$WORK_DIR/start.log" 2>&1; then exit 1; fi
export TRACE_T2_OTLP_PUBLISH_PORT="$(compose port alloy 4318 | sed 's/.*://')"
export TRACE_T2_OTLP="https://127.0.0.1:${TRACE_T2_OTLP_PUBLISH_PORT}/v1/traces" TRACE_T2_QUERY="https://$(compose port trace-query 8443)" TRACE_T2_LOKI="http://$(compose port runtime-log-api 3100)"
if ! (cd "$ROOT_DIR/monitor/backend"; go test ./internal/service -run '^TestOwnedTrace(Process|Pipeline)$' -count=1 -v -timeout=9m) >"$WORK_DIR/integration.log" 2>&1; then exit 1; fi
cat "$WORK_DIR/integration.log"
