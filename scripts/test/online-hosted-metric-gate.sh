#!/usr/bin/env bash
# Disposable metric revision lifecycle through GitHub Hosted Ubuntu.
# ADDP_ONLINE_SUITES=metric-service-revision-lifecycle
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=metric-service-revision-lifecycle
ADDP_ONLINE_METRIC_ENGINE_TYPE=${ADDP_ONLINE_METRIC_ENGINE_TYPE:-}
case "$ADDP_ONLINE_METRIC_ENGINE_TYPE" in
  postgresql|tidb) ;;
  *) echo "Metric Online gate requires postgresql or tidb" >&2; exit 1 ;;
esac
export ADDP_ONLINE_METRIC_ENGINE_TYPE
TIDB_COMPOSE_PROJECT=addp-online-metric-tidb
TIDB_COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.tidb-t2.yml"
TIDB_T2_PORT=55434
TIDB_MYSQL_IMAGE=mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b
if [ "$ADDP_ONLINE_METRIC_ENGINE_TYPE" = tidb ]; then
  for kind in container network volume; do
    if [ "$kind" = container ]; then
      remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$TIDB_COMPOSE_PROJECT") || exit 1
    else
      remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$TIDB_COMPOSE_PROJECT") || exit 1
    fi
    if [ -n "$remaining" ]; then
      echo "Metric Online gate refuses an existing TiDB $kind" >&2
      exit 1
    fi
  done
fi
HOSTED_FIXTURE_CONTAINERS=(addp-metric-online-disposable)
for service in tidb-pd tidb-tikv tidb; do
  HOSTED_FIXTURE_CONTAINERS+=("$TIDB_COMPOSE_PROJECT-$service-1")
done
HOSTED_FIXTURE_IMAGES=()
tidb_compose() {
  TIDB_T2_PORT="$TIDB_T2_PORT" docker compose -p "$TIDB_COMPOSE_PROJECT" -f "$TIDB_COMPOSE_FILE" "$@"
}
tidb_mysql() {
  docker run --rm -i --network "${TIDB_COMPOSE_PROJECT}_default" "$TIDB_MYSQL_IMAGE" \
    mysql -htidb -P4000 -uroot --protocol=tcp --connect-timeout=5 --default-character-set=utf8mb4 "$@"
}
start_tidb_fixture() {
  [ -d "$ADDP_ONLINE_SECRET_DIR" ] || fail "Hosted secret directory is missing"
  tidb_compose up -d --force-recreate tidb-pd tidb-tikv tidb
  local ready=0
  for _ in $(seq 1 180); do
    if tidb_mysql --batch --skip-column-names -e 'SELECT 1' >/dev/null 2>&1; then ready=1; break; fi
    sleep 2
  done
  [ "$ready" = 1 ] || fail "metric TiDB cluster did not become ready"
  METRIC_TIDB_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
  export METRIC_TIDB_PASSWORD
  if ! tidb_mysql >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/metric-tidb-seed-error.log" <<SQL
CREATE DATABASE metric_fixture CHARACTER SET utf8mb4;
CREATE TABLE metric_fixture.metric_people (person_id VARCHAR(32) PRIMARY KEY);
CREATE TABLE metric_fixture.metric_events (event_id VARCHAR(32) PRIMARY KEY, event_date DATE NOT NULL);
CREATE TABLE metric_fixture.metric_facts (row_id BIGINT PRIMARY KEY, person_id VARCHAR(32) NOT NULL, event_id VARCHAR(32) NOT NULL, leader TINYINT(1) NOT NULL);
INSERT INTO metric_fixture.metric_people VALUES ('A'), ('B');
INSERT INTO metric_fixture.metric_events VALUES ('e1','2026-01-01'), ('e2','2026-02-01'), ('e3','2026-03-01'), ('old','2025-12-31');
INSERT INTO metric_fixture.metric_facts VALUES (1,'A','e1',1), (2,'A','e1',1), (3,'A','e2',1), (4,'A','e3',0), (5,'A','old',1), (6,'B','e3',1);
CREATE USER 'metric_reader'@'%' IDENTIFIED BY '$METRIC_TIDB_PASSWORD';
GRANT SELECT ON metric_fixture.* TO 'metric_reader'@'%';
SQL
  then
    fail "metric TiDB seed could not be initialized"
  fi
  python3 - <<'PY'
import json, os
path = os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE']
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'name':'Hosted metric TiDB', 'engine_type':'tidb', 'engine_origin':'general',
               'description':'Disposable metric acceptance source', 'connection_info':{
                   'host':'127.0.0.1', 'port':55434, 'database':'metric_fixture', 'user':'metric_reader',
                   'password':os.environ['METRIC_TIDB_PASSWORD']}}, output)
PY
  unset METRIC_TIDB_PASSWORD
  echo "Disposable metric TiDB source is ready"
}
stop_online_fixture() {
  if [ "$ADDP_ONLINE_METRIC_ENGINE_TYPE" = postgresql ]; then
    run_logged bash business/scripts/online-metric-postgres-fixture.sh stop
    return
  fi
  run_logged tidb_compose down --volumes --remove-orphans
  local remaining
  remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$TIDB_COMPOSE_PROJECT") || return 1
  [ -z "$remaining" ] || return 1
  remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$TIDB_COMPOSE_PROJECT") || return 1
  [ -z "$remaining" ] || return 1
  remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$TIDB_COMPOSE_PROJECT") || return 1
  [ -z "$remaining" ]
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export STANDARD_URL=http://127.0.0.1:8110 MODEL_URL=http://127.0.0.1:8181
export SECURITY_URL=http://127.0.0.1:8194
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/metric-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
if [ "$ADDP_ONLINE_METRIC_ENGINE_TYPE" = postgresql ]; then
  run_logged bash business/scripts/online-metric-postgres-fixture.sh start
else
  run_logged start_tidb_fixture
fi
application_owned=1
for start_target in -model -service -security; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite metric-service-revision-lifecycle --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_CONSUMER_ENGINE_ID
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
