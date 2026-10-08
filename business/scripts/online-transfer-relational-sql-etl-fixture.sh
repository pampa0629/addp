#!/usr/bin/env bash
# Disposable PostgreSQL and MongoDB owner for Hosted Transfer lineage acceptance.

set -euo pipefail
umask 077

SOURCE_TABLE=addp_online_transfer_sql_etl_source
TARGET_TABLE=addp_online_transfer_sql_etl_target
NATIVE_TARGET=addp_online_transfer_field_lineage_target
MULTI_TARGET=addp_online_transfer_multi_query_target
NATIVE_DOWNSTREAM=addp_online_transfer_field_lineage_downstream
MONGODB_TARGET=addp_online_transfer_mongodb_ods
DIM_TARGET=addp_online_transfer_dim_activity
DWD_TARGET=addp_online_transfer_dwd_activity
WIDE_SOURCE=addp_wide_source
WIDE_TARGET=addp_wide_target
WIDE_DOWNSTREAM=addp_wide_downstream

fail() {
  echo "Online Transfer relational SQL ETL fixture failed: $*" >&2
  exit 1
}

[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "${ADDP_ONLINE_OWNER_MANAGED:-0}" != 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail "GitHub Hosted Linux x86_64 Online is required"

container=addp-transfer-online-disposable
mongodb_container=addp-transfer-mongodb-online-disposable
database=transfer_fixture
owner=transfer-relational-sql-etl
action=${1:-}
case "$action" in
  start|evolve|verify|stop|status) ;;
  *) fail "usage: bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start|evolve|verify|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

assert_owned() {
  local owned_container=${1:-$container}
  [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$owned_container")" = "$owner" ] || fail "container ownership mismatch"
}
postgres_running() {
  [ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ]
}
postgres_sql() {
  docker exec -i "$container" psql -v ON_ERROR_STOP=1 -U postgres -d "$database" "$@"
}

mongodb_shell() {
  { printf '%s\n' "if (!db.getSiblingDB('admin').auth(process.env.MONGO_INITDB_ROOT_USERNAME, process.env.MONGO_INITDB_ROOT_PASSWORD)) throw Error('fixture authentication failed');"; cat; } |
    docker exec -i -e TRANSFER_MONGODB_PASSWORD "$mongodb_container" mongosh --quiet --file /dev/stdin
}

start_mongodb() {
  export TRANSFER_MONGODB_PASSWORD MONGO_INITDB_ROOT_PASSWORD
  TRANSFER_MONGODB_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
  MONGO_INITDB_ROOT_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
  docker run -d --name "$mongodb_container" --label "com.addp.online-fixture=$owner" \
    --tmpfs /data/db --tmpfs /data/configdb -p 127.0.0.1:55434:27017 \
    -e MONGO_INITDB_ROOT_USERNAME=fixture_root -e MONGO_INITDB_ROOT_PASSWORD mongo:7.0 >/dev/null
  ready=0
  for _ in $(seq 1 60); do
    if mongodb_shell >/dev/null 2>&1 <<'JS'
if (db.getSiblingDB('admin').runCommand({ping: 1}).ok !== 1) throw Error('not ready');
JS
    then ready=1; break; fi
    sleep 1
  done
  [ "$ready" = 1 ] || fail "source MongoDB did not become ready"
  if ! mongodb_shell >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/mongodb-fixture-error.log" <<'JS'
const fixture = db.getSiblingDB('transfer_fixture');
fixture.activities.insertMany([
  {_id: 'activity-1', status: 'active', title: {date: '2026-01-01', level: 'easy'}, leader: {personid: 'person-1', userInfo: {nickName: 'Alice'}}},
  {_id: 'activity-2', status: 'inactive', title: {date: '2026-01-02', level: 'moderate'}, leader: {personid: 'person-2', userInfo: {nickName: 'Bob'}}},
  {_id: 'activity-3', status: 'active', title: {date: '2026-01-03', level: 'hard'}, leader: {personid: 'person-3', userInfo: {nickName: 'Carol'}}}
]);
fixture.createUser({user: 'transfer_reader', pwd: process.env.TRANSFER_MONGODB_PASSWORD, roles: [{role: 'read', db: 'transfer_fixture'}]});
JS
  then fail "MongoDB source permissions could not be initialized"; fi
  python3 - <<'PY_DESCRIPTOR'
import json, os
fd = os.open(os.environ['ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE'], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'name':'Hosted Transfer MongoDB', 'engine_type':'mongodb', 'engine_origin':'general',
               'description':'Disposable nested-field ODS lineage acceptance', 'connection_info':{
                   'host':'127.0.0.1', 'port':55434, 'database':'transfer_fixture', 'user':'transfer_reader',
                   'password':os.environ['TRANSFER_MONGODB_PASSWORD'], 'auth_source':'transfer_fixture'}}, output)
PY_DESCRIPTOR
  unset TRANSFER_MONGODB_PASSWORD MONGO_INITDB_ROOT_PASSWORD
}

reset_fixture() {
  postgres_sql <<SQL >/dev/null
DROP TABLE IF EXISTS public.${WIDE_DOWNSTREAM};
DROP TABLE IF EXISTS public.${WIDE_TARGET};
DROP TABLE IF EXISTS public.${WIDE_SOURCE};
DROP TABLE IF EXISTS public.${TARGET_TABLE};
DROP TABLE IF EXISTS public.${MULTI_TARGET};
DROP TABLE IF EXISTS public.${NATIVE_DOWNSTREAM};
DROP TABLE IF EXISTS public.${NATIVE_TARGET};
DROP TABLE IF EXISTS public.${SOURCE_TABLE};
DROP TABLE IF EXISTS public.${DWD_TARGET};
DROP TABLE IF EXISTS public.${DIM_TARGET};
DO \$wide\$
DECLARE columns_sql text; values_sql text;
BEGIN
  SELECT string_agg(format('%I bigint NOT NULL', 'field_' || lpad(i::text, 4, '0')), ',' ORDER BY i),
         string_agg(i::text, ',' ORDER BY i)
    INTO columns_sql, values_sql FROM generate_series(0,499) i;
  EXECUTE 'CREATE TABLE public.${WIDE_SOURCE} (' || columns_sql || ')';
  EXECUTE 'INSERT INTO public.${WIDE_SOURCE} VALUES (' || values_sql || '),(' || values_sql || ')';
END; \$wide\$;
CREATE TABLE public.${DIM_TARGET} (activity_id text PRIMARY KEY, activity_date date NOT NULL);
CREATE TABLE public.${DWD_TARGET} (activity_id text PRIMARY KEY, activity_date date NOT NULL, person_nickname text NOT NULL, intensity text NOT NULL);
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
  for wide_table in "$WIDE_TARGET" "$WIDE_DOWNSTREAM"; do
    columns=$(postgres_sql -Atc "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='$wide_table' AND data_type='bigint' AND is_nullable='NO'")
    [ "$columns" = 500 ] || fail "wide target must retain 500 non-null bigint columns: $wide_table"
    values=$(postgres_sql -Atc "SELECT COUNT(*) FROM ((SELECT to_jsonb(t) FROM public.$wide_table t EXCEPT ALL SELECT to_jsonb(s) FROM public.$WIDE_SOURCE s) UNION ALL (SELECT to_jsonb(s) FROM public.$WIDE_SOURCE s EXCEPT ALL SELECT to_jsonb(t) FROM public.$wide_table t)) differences")
    [ "$values" = 0 ] || fail "wide target complete row multiset differs: $wide_table"
  done
  values=$(postgres_sql -Atc "SELECT string_agg(CONCAT_WS('|', id, combined_label, combined_amount), ';' ORDER BY id, combined_label) FROM public.${MULTI_TARGET}")
  [ "$values" = '3|north:active|600.50;3|north:active|600.50;4|east:active|801.50;4|east:active|801.50' ] || fail "multi-source CTE/JOIN/UNION rows differ: $values"
  columns=$(postgres_sql -Atc "SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema='public' AND table_name='${MULTI_TARGET}'")
  [ "$columns" = 'id,combined_label,combined_amount' ] || fail "multi-source query output columns differ: $columns"

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
  values=$(postgres_sql -Atc "SELECT string_agg(CONCAT_WS('|', activity_id, activity_status, activity_date_raw, activity_level_raw, leader_person_id, leader_nickname_snapshot), ';' ORDER BY activity_id) FROM public.${MONGODB_TARGET}")
  [ "$values" = 'activity-1|active|2026-01-01|easy|person-1|Alice;activity-2|inactive|2026-01-02|moderate|person-2|Bob;activity-3|active|2026-01-03|hard|person-3|Carol' ] || fail "MongoDB ODS rows differ from the exact nested mapping: $values"
  columns=$(postgres_sql -Atc "SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '${MONGODB_TARGET}'")
  [ "$columns" = 'activity_id,activity_status,activity_date_raw,activity_level_raw,leader_person_id,leader_nickname_snapshot' ] || fail "MongoDB ODS columns differ from mapping: $columns"
  values=$(postgres_sql -Atc "SELECT string_agg(CONCAT_WS('|', activity_id, activity_date), ';' ORDER BY activity_id) FROM public.${DIM_TARGET}")
  [ "$values" = 'activity-1|2026-01-01;activity-2|2026-01-02;activity-3|2026-01-03' ] || fail "DIM rows differ from exact date conversion: $values"
  values=$(postgres_sql -Atc "SELECT string_agg(CONCAT_WS('|', activity_id, activity_date, person_display_name, intensity), ';' ORDER BY activity_id) FROM public.${DWD_TARGET}")
  [ "$values" = 'activity-1|2026-01-01 00:00:00|Alice|EASY;activity-3|2026-01-03 00:00:00|Carol|HARD' ] || fail "DWD rows differ from exact JOIN, filter and expression: $values"
  columns=$(postgres_sql -Atc "SELECT string_agg(table_name || ':' || data_type, ',' ORDER BY table_name) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('${DIM_TARGET}', '${DWD_TARGET}') AND column_name='activity_date'")
  [ "$columns" = 'addp_online_transfer_dim_activity:date,addp_online_transfer_dwd_activity:timestamp without time zone' ] || fail "DIM/DWD dates must use the expected physical types: $columns"
  columns=$(postgres_sql -Atc "SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema='public' AND table_name='${DWD_TARGET}'")
  [ "$columns" = 'activity_id,activity_date,person_display_name,intensity' ] || fail "evolved DWD columns differ: $columns"
}

case "$action" in
  start)
    for candidate in "$container" "$mongodb_container"; do
      ! docker container inspect "$candidate" >/dev/null 2>&1 || fail "refusing an existing source container"
    done
    [ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] || fail "Hosted secret directory is missing"
    case "$ADDP_ONLINE_SECRET_DIR" in
      "${RUNNER_TEMP:?}"/addp-online-secret-*) ;;
      *) fail "secret directory must be in Runner temporary storage" ;;
    esac
    [ "${ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE:?}" = "$ADDP_ONLINE_SECRET_DIR/transfer-engine.json" ] || fail "descriptor must use the Hosted secret directory"
    [ "${ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE:?}" = "$ADDP_ONLINE_SECRET_DIR/transfer-mongodb-engine.json" ] || fail "MongoDB descriptor must use the Hosted secret directory"
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
GRANT SELECT ON public.addp_online_transfer_sql_etl_source, public.addp_wide_source TO transfer_writer;
GRANT SELECT, INSERT, DELETE ON public.addp_online_transfer_dim_activity, public.addp_online_transfer_dwd_activity TO transfer_writer;
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
    start_mongodb
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
  evolve)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    columns=$(postgres_sql -Atc "SELECT string_agg(column_name || ':' || data_type, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema='public' AND table_name='${DWD_TARGET}'")
    [ "$columns" = 'activity_id:text,activity_date:date,person_nickname:text,intensity:text' ] || fail "schema evolution requires the original DWD structure: $columns"
    values=$(postgres_sql -Atc "SELECT string_agg(CONCAT_WS('|', activity_id, activity_date, person_nickname, intensity), ';' ORDER BY activity_id) FROM public.${DWD_TARGET}")
    [ "$values" = 'activity-1|2026-01-01|Alice|EASY;activity-3|2026-01-03|Carol|HARD' ] || fail "schema evolution requires exact completed DWD rows: $values"
    postgres_sql <<SQL_EVOLVE
BEGIN;
ALTER TABLE public.${DWD_TARGET} RENAME COLUMN person_nickname TO person_display_name;
ALTER TABLE public.${DWD_TARGET} ALTER COLUMN activity_date TYPE timestamp without time zone USING activity_date::timestamp;
COMMIT;
SQL_EVOLVE
    echo "Online Transfer DWD physical schema is evolved"
    ;;
  verify)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    assert_owned "$mongodb_container"
    verify_fixture
    echo "Online Transfer relational SQL ETL target is verified"
    ;;
  stop)
    # Check ownership before deleting either container; cleanup also covers partial startup.
    for candidate in "$container" "$mongodb_container"; do
      if docker container inspect "$candidate" >/dev/null 2>&1; then assert_owned "$candidate"; fi
    done
    cleanup_failed=0
    for candidate in "$container" "$mongodb_container"; do
      if docker container inspect "$candidate" >/dev/null 2>&1; then
        if ! docker rm -fv "$candidate" >/dev/null; then cleanup_failed=1; fi
      fi
      if ! remaining=$(docker ps -aq --filter "name=^/${candidate}$"); then
        echo "cannot verify container cleanup: $candidate" >&2
        cleanup_failed=1
      elif [ -n "$remaining" ]; then
        echo "container remains after cleanup: $candidate" >&2
        cleanup_failed=1
      fi
    done
    [ "$cleanup_failed" = 0 ] || fail "fixture cleanup is incomplete"
    echo "Online Transfer relational SQL ETL fixture is stopped; zero residuals"
    ;;
  status)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    assert_owned "$mongodb_container"
    mongodb_shell <<'JS' | grep -qx '3' || fail "source collection is not ready"
print(db.getSiblingDB('transfer_fixture').activities.countDocuments({}));
JS
    postgres_sql -Atc "SELECT COUNT(*) FROM public.${SOURCE_TABLE}" | grep -qx '5' || fail "source table is not ready"
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
esac
