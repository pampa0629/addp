#!/usr/bin/env bash
# Disposable PostgreSQL owner for Hosted SQL ETL and field lineage acceptance.

set -euo pipefail
umask 077

SOURCE_TABLE=addp_online_transfer_sql_etl_source
TARGET_TABLE=addp_online_transfer_sql_etl_target
NATIVE_TARGET=addp_online_transfer_field_lineage_target
NATIVE_DOWNSTREAM=addp_online_transfer_field_lineage_downstream

fail() {
  echo "Online Transfer relational SQL ETL fixture failed: $*" >&2
  exit 1
}

[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "${ADDP_ONLINE_OWNER_MANAGED:-0}" != 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail "GitHub Hosted Linux x86_64 Online is required"

container=addp-transfer-online-disposable
database=transfer_fixture
owner=transfer-relational-sql-etl
action=${1:-}
case "$action" in
  start|verify|stop|status) ;;
  *) fail "usage: bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start|verify|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

assert_owned() {
  [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = "$owner" ] || fail "container ownership mismatch"
}
postgres_running() {
  [ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ]
}
postgres_sql() {
  docker exec -i "$container" psql -v ON_ERROR_STOP=1 -U postgres -d "$database" "$@"
}

reset_fixture() {
  postgres_sql <<SQL >/dev/null
DROP TABLE IF EXISTS public.${TARGET_TABLE};
DROP TABLE IF EXISTS public.${NATIVE_DOWNSTREAM};
DROP TABLE IF EXISTS public.${NATIVE_TARGET};
DROP TABLE IF EXISTS public.${SOURCE_TABLE};
CREATE TABLE public.${SOURCE_TABLE} (
  id bigint PRIMARY KEY,
  region varchar(32) NOT NULL,
  status varchar(16) NOT NULL,
  amount numeric(10,2) NOT NULL,
  internal_note varchar(64) NOT NULL
);
INSERT INTO public.${SOURCE_TABLE} (id, region, status, amount, internal_note) VALUES
  (1, 'north', 'active', 10.50, 'hidden-1'),
  (2, 'south', 'inactive', 200.00, 'hidden-2'),
  (3, 'north', 'active', 300.25, 'hidden-3'),
  (4, 'east', 'active', 400.75, 'hidden-4'),
  (5, 'west', 'inactive', 500.00, 'hidden-5');
SQL
}

verify_fixture() {
  local values columns
  values=$(postgres_sql -Atc \
    "SELECT CONCAT_WS('|', COUNT(*), MIN(id), MAX(id), SUM(amount)::numeric(12,2)) FROM public.${TARGET_TABLE}")
  [ "$values" = "2|3|4|701.00" ] || fail "target rows do not prove SQL filter semantics: $values"
  columns=$(postgres_sql -Atc \
    "SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '${TARGET_TABLE}'")
  [ "$columns" = "id,region,amount" ] || fail "target columns do not prove SQL projection semantics: $columns"
  values=$(postgres_sql -Atc \
    "SELECT CONCAT_WS('|', COUNT(*), SUM(amount)::numeric(12,2), COUNT(*) FILTER (WHERE region_name = 'active'), COUNT(*) FILTER (WHERE region_name = 'inactive'), COUNT(*) FILTER (WHERE generated_label = 'online')) FROM public.${NATIVE_TARGET}")
  [ "$values" = "5|1411.50|3|2|5" ] || fail "native target does not prove replace, precision and generated values: $values"
  values=$(postgres_sql -Atc \
    "SELECT CONCAT_WS('|', COUNT(*), SUM(amount)::numeric(12,2), COUNT(*) FILTER (WHERE area = 'active'), COUNT(*) FILTER (WHERE area = 'inactive')) FROM public.${NATIVE_DOWNSTREAM}")
  [ "$values" = "5|1411.50|3|2" ] || fail "downstream does not prove two-hop native mapping: $values"
  columns=$(postgres_sql -Atc \
    "SELECT CONCAT_WS('|', numeric_precision, numeric_scale) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '${NATIVE_TARGET}' AND column_name = 'amount'")
  [ "$columns" = "8|2" ] || fail "native target decimal definition differs from mapping: $columns"
}

case "$action" in
  start)
    ! docker container inspect "$container" >/dev/null 2>&1 || fail "refusing an existing source container"
    [ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] || fail "Hosted secret directory is missing"
    case "$ADDP_ONLINE_SECRET_DIR" in
      "${RUNNER_TEMP:?}"/addp-online-secret-*) ;;
      *) fail "secret directory must be in Runner temporary storage" ;;
    esac
    [ "${ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE:?}" = "$ADDP_ONLINE_SECRET_DIR/transfer-engine.json" ] || fail "descriptor must use the Hosted secret directory"
    export TRANSFER_FIXTURE_PASSWORD
    TRANSFER_FIXTURE_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
    env POSTGRES_PASSWORD="$TRANSFER_FIXTURE_PASSWORD" docker run -d --name "$container" \
      --label "com.addp.online-fixture=$owner" --tmpfs /var/lib/postgresql/data \
      -p 127.0.0.1:55433:5432 -e POSTGRES_PASSWORD -e POSTGRES_DB="$database" \
      addp-postgres-pgvector:latest >/dev/null
    ready=0
    for _ in $(seq 1 60); do
      if docker exec "$container" pg_isready -U postgres -d "$database" >/dev/null 2>&1; then ready=1; break; fi
      sleep 1
    done
    [ "$ready" = 1 ] || fail "source PostgreSQL did not become ready"
    reset_fixture
    if ! postgres_sql -v writer_password="$TRANSFER_FIXTURE_PASSWORD" >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/fixture-error.log" <<'SQL'
CREATE ROLE transfer_writer LOGIN PASSWORD :'writer_password';
GRANT CONNECT ON DATABASE transfer_fixture TO transfer_writer;
GRANT USAGE, CREATE ON SCHEMA public TO transfer_writer;
GRANT SELECT ON public.addp_online_transfer_sql_etl_source TO transfer_writer;
SQL
    then
      fail "source permissions could not be initialized"
    fi
    python3 - <<'PY_DESCRIPTOR'
import json, os
path = os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE']
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'name':'Hosted Transfer PostgreSQL', 'engine_type':'postgresql', 'engine_origin':'general',
               'description':'Disposable SQL ETL and field lineage acceptance', 'connection_info':{
                   'host':'127.0.0.1', 'port':55433, 'database':'transfer_fixture', 'user':'transfer_writer',
                   'password':os.environ['TRANSFER_FIXTURE_PASSWORD'], 'sslmode':'disable'}}, output)
PY_DESCRIPTOR
    unset TRANSFER_FIXTURE_PASSWORD
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
  verify)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    verify_fixture
    echo "Online Transfer relational SQL ETL target is verified"
    ;;
  stop)
    if docker container inspect "$container" >/dev/null 2>&1; then
      assert_owned
      # Removing the exclusive tmpfs container also removes all four owned tables,
      # including partial output after a failed task or interrupted startup.
      docker rm -fv "$container" >/dev/null
    fi
    remaining=$(docker ps -aq --filter "name=^/${container}$") || fail "cannot verify container cleanup"
    [ -z "$remaining" ] || fail "container remains after cleanup"
    echo "Online Transfer relational SQL ETL fixture is stopped; zero residuals"
    ;;
  status)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    postgres_sql -Atc "SELECT COUNT(*) FROM public.${SOURCE_TABLE}" | grep -qx '5' || fail "source table is not ready"
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
esac
