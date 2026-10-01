#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=minio,runtime-log-store-init,loki,runtime-log-api,alloy,runtime-log-pruner
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.system-runtime-log-t2.yml
# ADDP_T2_INPUT_FILES=scripts/infra/Dockerfile.minio scripts/infra/Dockerfile.runtime-log scripts/infra/runtime-logs.yml scripts/infra/loki.yml scripts/infra/runtime-logs.alloy scripts/infra/runtime-log-api.conf.template scripts/infra/init-runtime-log-store.sh common/go.mod common/go.sum common/runtimelog/capture.go common/runtimelog/capture_test.go common/cmd/runtime-log/main.go common/cmd/runtime-log/main_test.go
# Own disposable Compose startup, source files and teardown.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-runtime-log-t2.XXXXXX")
RUN_ID=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
COMPOSE_PROJECT="addp-runtime-log-t2-$RUN_ID"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.system-runtime-log-t2.yml"
export LOKI_TEST_SECRET="$RUN_ID" LOKI_READ_TOKEN="$RUN_ID-read" LOKI_WRITE_TOKEN="$RUN_ID-write"
export LOKI_S3_ACCESS_KEY=test-logs LOKI_S3_SECRET_KEY="$RUN_ID" LOKI_RETENTION_HOURS=168
export LOKI_TEST_SOURCE="$WORK_DIR/source"
mkdir -p "$LOKI_TEST_SOURCE"
compose(){ docker compose --env-file /dev/null -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }
cleanup(){
 local result=$?
 trap - EXIT INT TERM
 set +e
 if [ "$result" -ne 0 ];then compose logs --no-color >"$WORK_DIR/container.log" 2>&1; fi
 compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || result=1
 for resource in container network volume; do
  case "$resource" in
   container) remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT");;
   network) remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT");;
   volume) remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT");;
  esac
  [ -z "$remaining" ] || result=1
 done
 if [ "$result" -ne 0 ];then
   python3 - "$WORK_DIR" <<'PYERROR'
from pathlib import Path
import os,sys
for name in ['build.log','start.log','restart.log','producer.log','container.log','cleanup.log','outage.log']:
 p=Path(sys.argv[1])/name
 if not p.exists():continue
 text=p.read_text(errors='replace')[-4000:]
 for key in ['LOKI_TEST_SECRET','LOKI_READ_TOKEN','LOKI_WRITE_TOKEN']:
  secret=os.environ.get(key)
  if secret:text=text.replace(secret,'[REDACTED]')
 print(name+':\n'+text,file=sys.stderr)
PYERROR
 fi
 rm -rf "$WORK_DIR"
 exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
echo "Runtime log T2: build owned components"
compose build minio runtime-log-store-init runtime-log-pruner >"$WORK_DIR/build.log" 2>&1 || exit 1
compose up -d --wait --wait-timeout 180 minio loki runtime-log-api alloy runtime-log-pruner >"$WORK_DIR/start.log" 2>&1 || exit 1
export LOKI_TEST_URL="http://$(compose port runtime-log-api 3100)"
compose run --rm --no-deps runtime-log-pruner launch --module manager --role backend -- sh -c 'printf "{\"level\":\"error\",\"msg\":\"runtime-t2-first password=sample-secret\"}\n"; printf "plain stderr\n" >&2; sleep 2' >"$WORK_DIR/producer.log" 2>&1 || exit 1
python3 "$ROOT_DIR/scripts/test/runtime-log-probe.py" "$LOKI_TEST_SOURCE" first
compose up -d --force-recreate --wait --wait-timeout 90 loki runtime-log-api alloy >"$WORK_DIR/restart.log" 2>&1 || exit 1
export LOKI_TEST_URL="http://$(compose port runtime-log-api 3100)"
python3 "$ROOT_DIR/scripts/test/runtime-log-probe.py" "$LOKI_TEST_SOURCE" retained
compose run --rm --no-deps runtime-log-pruner launch --module manager --role backend -- sh -c 'printf "runtime-t2-second\n"; sleep 2' >"$WORK_DIR/producer.log" 2>&1 || exit 1
python3 "$ROOT_DIR/scripts/test/runtime-log-probe.py" "$LOKI_TEST_SOURCE" restarted
export ALLOY_TEST_URL="http://$(compose port alloy 12345)"
python3 "$ROOT_DIR/scripts/test/runtime-log-probe.py" "$LOKI_TEST_SOURCE" retry-baseline >"$WORK_DIR/retries"
compose stop runtime-log-api >"$WORK_DIR/outage.log" 2>&1 || exit 1
compose run --rm --no-deps runtime-log-pruner launch --module manager --role backend -- sh -c 'printf "runtime-t2-outage\n"; sleep 2' >"$WORK_DIR/producer.log" 2>&1 || exit 1
python3 "$ROOT_DIR/scripts/test/runtime-log-probe.py" "$LOKI_TEST_SOURCE" retry-observed "$(cat "$WORK_DIR/retries")"
compose up -d --wait --wait-timeout 60 runtime-log-api >>"$WORK_DIR/outage.log" 2>&1 || exit 1
export LOKI_TEST_URL="http://$(compose port runtime-log-api 3100)"
python3 "$ROOT_DIR/scripts/test/runtime-log-probe.py" "$LOKI_TEST_SOURCE" outage-recovered
echo "Runtime log identity, authorization, collection, persistence and old-instance isolation passed"
