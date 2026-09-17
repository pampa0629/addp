#!/usr/bin/env bash
# Disposable PostgreSQL source for the Hosted metric lifecycle acceptance.
set -euo pipefail
fail() { echo "Metric PostgreSQL fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] || fail "Hosted Linux Online is required"
container=addp-metric-online-disposable
case "${1:-}" in
  stop)
    if docker container inspect "$container" >/dev/null 2>&1; then
      [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = metric ] || fail "container ownership mismatch"
      docker rm -fv "$container" >/dev/null
    fi
    ! docker container inspect "$container" >/dev/null 2>&1 || fail "container remains after cleanup"
    exit 0
    ;;
  start) ;;
  *) fail "usage: online-metric-postgres-fixture.sh start|stop" ;;
esac
! docker container inspect "$container" >/dev/null 2>&1 || fail "refusing an existing source container"
[ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] || fail "Hosted secret directory is missing"
export POSTGRES_PASSWORD
POSTGRES_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
# Infra has just built this repository-owned image on the clean Hosted VM.
docker run -d --name "$container" --label com.addp.online-fixture=metric \
  -p 127.0.0.1:55433:5432 -e POSTGRES_PASSWORD -e POSTGRES_DB=metric_fixture \
  addp-postgres-pgvector:latest >/dev/null
ready=0
for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -U postgres -d metric_fixture >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || fail "source PostgreSQL did not become ready"
docker exec -i "$container" psql -U postgres -d metric_fixture -v ON_ERROR_STOP=1 \
  -v reader_password="$POSTGRES_PASSWORD" >/dev/null <<'SQL'
CREATE TABLE metric_people (person_id text PRIMARY KEY);
CREATE TABLE metric_events (event_id text PRIMARY KEY, event_date date NOT NULL);
CREATE TABLE metric_facts (row_id bigint PRIMARY KEY, person_id text NOT NULL, event_id text NOT NULL, leader boolean NOT NULL);
INSERT INTO metric_people VALUES ('A'), ('B');
INSERT INTO metric_events VALUES ('e1','2026-01-01'), ('e2','2026-02-01'), ('e3','2026-03-01'), ('old','2025-12-31');
INSERT INTO metric_facts VALUES (1,'A','e1',true), (2,'A','e1',true), (3,'A','e2',true), (4,'A','e3',false), (5,'A','old',true), (6,'B','e3',true);
CREATE ROLE metric_reader LOGIN PASSWORD :'reader_password';
GRANT CONNECT ON DATABASE metric_fixture TO metric_reader;
GRANT USAGE ON SCHEMA public TO metric_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO metric_reader;
SQL
python3 - <<'PY'
import json, os
path = os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE']
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'name':'Hosted metric PostgreSQL', 'engine_type':'postgresql', 'engine_origin':'general',
               'description':'Disposable metric acceptance source', 'connection_info':{
                   'host':'127.0.0.1', 'port':55433, 'database':'metric_fixture', 'user':'metric_reader',
                   'password':os.environ['POSTGRES_PASSWORD'], 'sslmode':'disable'}}, output)
PY
echo "Disposable metric PostgreSQL source is ready"
