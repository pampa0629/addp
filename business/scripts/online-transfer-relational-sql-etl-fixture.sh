#!/usr/bin/env bash
# Disposable source and write-failure target owner for Hosted Transfer acceptance.

set -euo pipefail
umask 077

SOURCE_TABLE=addp_online_transfer_sql_etl_source
TARGET_TABLE=addp_online_transfer_sql_etl_target
IMPORT_TARGET=addp_online_manager_import_points
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
doris_container=addp-transfer-doris-online-disposable
clickhouse_container=addp-transfer-clickhouse-online-disposable
minio_container=addp-transfer-upload-online-disposable
database=transfer_fixture
owner=transfer-relational-sql-etl
action=${1:-}
case "$action" in
  start|evolve|write-failures|manager-import|object-facts|verify|stop|status) ;;
  *) fail "usage: bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start|evolve|write-failures|manager-import|object-facts|verify|stop|status" ;;
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

mongodb_readiness_diagnostics() {
  # Keep raw driver/container output in the non-archived secret directory.
  # Only fixed error categories and non-secret process state reach CI logs.
  docker inspect --format 'MongoDB state: running={{.State.Running}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}' "$mongodb_container" >&2 || true
  docker logs "$mongodb_container" >"$ADDP_ONLINE_SECRET_DIR/mongodb-startup.log" 2>&1 || true
  python3 - "$ADDP_ONLINE_SECRET_DIR" <<'PY_DIAGNOSTICS'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
for name in ('mongodb-readiness-error.log', 'mongodb-fixture-error.log', 'mongodb-startup.log'):
    path = root / name
    if not path.exists():
        continue
    content = path.read_text(errors='replace').lower()
    categories = [label for label, markers in (
        ('connection-refused', ('econnrefused', 'connection refused')),
        ('connection-closed', ('econnreset', 'connection closed', 'socket hang up')),
        ('authentication-failed', ('authentication failed', 'fixture authentication failed')),
        ('missing-file', ('enoent', 'no such file or directory')),
        ('permission-denied', ('eacces', 'permission denied', 'operation not permitted')),
        ('storage-full', ('no space left on device',)),
        ('illegal-instruction', ('illegal instruction',)),
        ('storage-error', ('wiredtiger error',)),
    ) if any(marker in content for marker in markers)]
    print(f'MongoDB diagnostic {name}: {",".join(categories) or "unclassified"}', file=sys.stderr)
PY_DIAGNOSTICS
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
    # The entrypoint's temporary init server can already authenticate and ping.
    # Wait for its final exec before probing or writing the one-shot fixture.
    if docker exec "$mongodb_container" sh -c '[ "$(cat /proc/1/comm)" = mongod ]' \
      >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/mongodb-readiness-error.log" &&
      mongodb_shell >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/mongodb-readiness-error.log" <<'JS'
if (db.getSiblingDB('admin').runCommand({ping: 1}).ok !== 1) throw Error('not ready');
JS
    then ready=1; break; fi
    sleep 1
  done
  if [ "$ready" != 1 ]; then
    mongodb_readiness_diagnostics
    fail "source MongoDB did not become ready"
  fi
  if ! mongodb_shell >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/mongodb-fixture-error.log" <<'JS'
const fixture = db.getSiblingDB('transfer_fixture');
fixture.activities.insertMany([
  {_id: 'activity-1', status: 'active', title: {date: '2026-01-01', level: 'easy'}, leader: {personid: 'person-1', userInfo: {nickName: 'Alice'}}},
  {_id: 'activity-2', status: 'inactive', title: {date: '2026-01-02', level: 'moderate'}, leader: {personid: 'person-2', userInfo: {nickName: 'Bob'}}},
  {_id: 'activity-3', status: 'active', title: {date: '2026-01-03', level: 'hard'}, leader: {personid: 'person-3', userInfo: {nickName: 'Carol'}}}
]);
fixture.createUser({user: 'transfer_reader', pwd: process.env.TRANSFER_MONGODB_PASSWORD, roles: [{role: 'read', db: 'transfer_fixture'}]});
JS
  then
    mongodb_readiness_diagnostics
    fail "MongoDB source permissions could not be initialized"
  fi
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


doris_sql() {
  docker exec -i "$doris_container" mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=5 --batch --skip-column-names "$@"
}
clickhouse_sql() {
  docker exec -i "$clickhouse_container" sh -c 'exec clickhouse-client --user default --password "$CLICKHOUSE_PASSWORD" "$@"' _ "$@"
}

start_write_failure_targets() {
  local business_dir ready=0
  business_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
  # Reuse the Business-owned FE/BE lifecycle, including stable Backend identity.
  docker run -d --name "$doris_container" --label "com.addp.online-fixture=$owner" \
    --add-host host.docker.internal:127.0.0.1 -p 127.0.0.1::9030 -e BUILD_TYPE=production \
    --mount "type=bind,src=$business_dir/doris/init.sh,dst=/addp-doris-init.sh,readonly" \
    --entrypoint /bin/bash \
    apache/doris:doris-all-in-one-2.1.0@sha256:83538f071c8ea22134b8151d8b751cdf36da9e1a5da315443ba461909ee11835 \
    /addp-doris-init.sh >/dev/null
  for _ in $(seq 1 120); do
    if docker exec "$doris_container" bash /addp-doris-init.sh --health > /dev/null 2>&1; then ready=1; break; fi
    sleep 2
  done
  [ "$ready" = 1 ] || fail "Doris write-failure target did not become ready"
  export TRANSFER_DORIS_PORT
  TRANSFER_DORIS_PORT=$(docker port "$doris_container" 9030/tcp); TRANSFER_DORIS_PORT=${TRANSFER_DORIS_PORT##*:}
  export CLICKHOUSE_PASSWORD TRANSFER_CLICKHOUSE_PORT
  CLICKHOUSE_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
  docker run -d --name "$clickhouse_container" --label "com.addp.online-fixture=$owner" \
    -p 127.0.0.1::9000 -e CLICKHOUSE_USER=default -e CLICKHOUSE_PASSWORD \
    -e CLICKHOUSE_DB=transfer_fixture -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
    clickhouse/clickhouse-server:25.12.1.649@sha256:7234d193fbeb3a375f21f0bb99040396f68bb2e82cada4f4947d7fcf9638bbf3 >/dev/null
  ready=0
  for _ in $(seq 1 120); do
    if docker exec "$clickhouse_container" sh -c 'test "$(tr "\000" "\n" < /proc/1/cmdline | head -n 1)" = clickhouse-server' &&
        clickhouse_sql --query 'SELECT 1' 2>/dev/null | grep -qx '1'; then ready=1; break; fi
    sleep 2
  done
  [ "$ready" = 1 ] || fail "ClickHouse write-failure target did not become ready"
  TRANSFER_CLICKHOUSE_PORT=$(docker port "$clickhouse_container" 9000/tcp); TRANSFER_CLICKHOUSE_PORT=${TRANSFER_CLICKHOUSE_PORT##*:}
  doris_sql -e 'CREATE DATABASE transfer_fixture; SET GLOBAL enable_insert_strict=true;' >/dev/null
  for target in addp_online_transfer_write_failure addp_online_transfer_json_failure; do
    doris_sql -e "CREATE TABLE transfer_fixture.$target (id BIGINT NOT NULL, document STRING NULL, payload STRING NULL, label STRING NULL) DUPLICATE KEY(id) PARTITION BY RANGE(id) (PARTITION bounded_write VALUES LESS THAN (\"1001\")) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES (\"replication_num\"=\"1\"); INSERT INTO transfer_fixture.$target VALUES (0, '{\"sentinel\":true}', UNHEX('00ff41'), 'plain {');" >/dev/null
    clickhouse_sql --multiquery --query "CREATE TABLE transfer_fixture.$target (id Int64, document Nullable(String), payload Nullable(String), label Nullable(String), CONSTRAINT fixture_label_length CHECK length(label) <= 8) ENGINE=MergeTree ORDER BY tuple(); INSERT INTO transfer_fixture.$target VALUES (0, '{\"sentinel\":true}', unhex('00ff41'), 'plain {');" >/dev/null
  done
  python3 - <<'PY_DESCRIPTOR_FAILURES'
import json, os
from pathlib import Path
for engine_type, port, user, password in (
    ('doris', os.environ['TRANSFER_DORIS_PORT'], 'root', ''),
    ('clickhouse', os.environ['TRANSFER_CLICKHOUSE_PORT'], 'default', os.environ['CLICKHOUSE_PASSWORD']),
):
    path = Path(os.environ['ADDP_ONLINE_SECRET_DIR']) / f'transfer-{engine_type}-engine.json'
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump({'name': f'Hosted Transfer {engine_type}', 'engine_type': engine_type, 'engine_origin': 'general',
                   'description': 'Disposable bounded write-failure acceptance', 'connection_info': {
                       'host': '127.0.0.1', 'port': int(port), 'database': 'transfer_fixture', 'user': user, 'password': password}}, output)
PY_DESCRIPTOR_FAILURES
  unset CLICKHOUSE_PASSWORD TRANSFER_DORIS_PORT TRANSFER_CLICKHOUSE_PORT
}

verify_write_failures() {
  local engine target scenario want values separator=''
  printf '{'
  for engine in doris clickhouse; do
    if [ "$engine" = doris ]; then assert_owned "$doris_container"; else assert_owned "$clickhouse_container"; fi
    printf '%s"%s":{' "$separator" "$engine"
    separator=','
    local case_separator=''
    for scenario in late_execute_failure invalid_json; do
      if [ "$scenario" = late_execute_failure ]; then target=addp_online_transfer_write_failure; want='1001\t1\t1000\t0'; else target=addp_online_transfer_json_failure; want='1\t1\t0\t0'; fi
      query="SELECT COUNT(*), COALESCE(SUM(CASE WHEN id=0 AND document='{\"sentinel\":true}' AND HEX(payload)='00FF41' AND label='plain {' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN id BETWEEN 1 AND 1000 AND document='{\"ok\":true}' AND HEX(payload)='00FF41' AND label='plain {' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN id=1001 THEN 1 ELSE 0 END),0) FROM transfer_fixture.$target"
      if [ "$engine" = doris ]; then values=$(doris_sql -e "$query"); else values=$(clickhouse_sql --query "$query"); fi
      [ "$values" = "$(printf "$want")" ] || fail "write-failure physical row evidence mismatch: $engine/$scenario"
      printf '%s"%s":{"rows":%s,"sentinel_rows":1,"valid_rows":%s,"failed_row_absent":true}' "$case_separator" "$scenario" "$(cut -f1 <<< "$values")" "$(cut -f3 <<< "$values")"
      case_separator=','
    done
    printf '}'
  done
  printf '}\n'
}

reset_fixture() {
  postgres_sql <<SQL >/dev/null
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE TABLE public.addp_online_transfer_failure_source (id bigint PRIMARY KEY, document text NOT NULL, payload bytea NOT NULL, label text NOT NULL);
INSERT INTO public.addp_online_transfer_failure_source
SELECT i, '{"ok":true}', decode('00ff41','hex'), CASE WHEN i=1001 THEN 'synthetic constraint failure' ELSE 'plain {' END FROM generate_series(1,1001) i;
CREATE TABLE public.addp_online_transfer_invalid_json_source (LIKE public.addp_online_transfer_failure_source INCLUDING ALL);
INSERT INTO public.addp_online_transfer_invalid_json_source
SELECT i, CASE WHEN i=1001 THEN '{' ELSE '{"ok":true}' END, decode('00ff41','hex'), 'plain {' FROM generate_series(1,1001) i;
DROP TABLE IF EXISTS public.${IMPORT_TARGET};
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

verify_manager_import() {
  # Discover the geometry column from PostGIS; no storage field-name assumption.
  postgres_sql <<'SQL' >/dev/null
DO $import$
DECLARE geometry_field text; geometry_count int; actual text;
BEGIN
  SELECT min(f_geometry_column), count(*) INTO geometry_field, geometry_count
    FROM public.geometry_columns WHERE f_table_schema='public' AND f_table_name='addp_online_manager_import_points';
  IF geometry_count <> 1 THEN RAISE EXCEPTION 'Manager import must create one geometry column'; END IF;
  EXECUTE format('SELECT string_agg(concat_ws(''|'', id, label, ST_AsText(%1$I), ST_SRID(%1$I)), '';'' ORDER BY id) FROM public.addp_online_manager_import_points', geometry_field) INTO actual;
  IF actual IS DISTINCT FROM '1|alpha|POINT(10 20)|4326;2|beta|POINT(30 40)|4326' THEN
    RAISE EXCEPTION 'Manager import rows or geometry differ';
  END IF;
END; $import$;
SQL
}

verify_fixture() {
  local values columns
  verify_manager_import
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


object_storage_fixture() {
  python3 - "$1" <<'PY_OBJECT'
import hashlib, importlib, json, os, secrets, subprocess, sys
from pathlib import Path
shared = importlib.import_module('business.scripts.online-raster-minio-fixture')
root = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
name, owner, bucket = 'addp-transfer-upload-online-disposable', 'transfer-relational-sql-etl', 'transfer-upload'
def mc(*arguments, binary=False):
    result = subprocess.run(['docker', 'exec', name, 'sh', '-ec',
        'export MC_HOST_fixture="http://$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD@127.0.0.1:9000"; exec mc "$@"', '_', *arguments], capture_output=True, timeout=20)
    if result.returncode:
        raise shared.FixtureError('object storage physical command failed')
    return result.stdout if binary else result.stdout.decode()
if sys.argv[1] == 'start':
    port = int(os.environ.get('ADDP_ONLINE_TRANSFER_MINIO_PORT', '59032'))
    if not 1024 <= port <= 65535: raise shared.FixtureError('invalid fixture MinIO port')
    entry = {'endpoint': f'127.0.0.1:{port}', 'access_key': 'transfer-upload', 'secret_key': secrets.token_hex(32), 'bucket': bucket}
    shared.start_minio(root, entry, name, owner)
    mc('mb', 'fixture/' + bucket)
    shared.private_json(root / 'transfer-minio-engine.json', {
        'name': 'Hosted Transfer MinIO', 'engine_type': 'minio', 'engine_origin': 'general',
        'description': 'Disposable Manager object upload source facts acceptance',
        'connection_info': {key: entry[key] for key in ('endpoint','access_key','secret_key')} | {'use_ssl': False}})
else:
    expected = {'upload-check.csv','points.shp','points.shx','points.dbf','points.cpg','points.prj'}
    pending = mc('--json', 'ls', '--recursive', 'fixture/' + bucket + '/').strip()
    entries = []
    while pending:
        entry, offset = json.JSONDecoder().raw_decode(pending)
        entries.append(entry)
        pending = pending[offset:].lstrip()
    if len(entries) != len(expected) or any(entry.get('status') != 'success' or entry.get('type') != 'file' for entry in entries) or {entry.get('key') for entry in entries} != expected:
        raise shared.FixtureError('physical object listing differs from the exact upload scope')
    objects = {}
    for filename in sorted(expected):
        physical = json.loads(mc('--json', 'stat', '--no-list', 'fixture/' + bucket + '/' + filename))
        if physical.get('status') != 'success' or physical.get('type') != 'file':
            raise shared.FixtureError('physical object stat is not a file')
        content = mc('cat', 'fixture/' + bucket + '/' + filename, binary=True)
        size = physical.get('size')
        if type(size) is not int or size <= 0 or len(content) != size:
            raise shared.FixtureError('physical object size differs from downloaded bytes')
        objects[filename] = {'size_bytes': size, 'etag': physical.get('etag'), 'last_modified': physical.get('lastModified'),
                            'sha256': hashlib.sha256(content).hexdigest()}
    print(json.dumps({'schema_version': 'addp.manager-object-physical/v1', 'bucket': bucket, 'objects': objects}, sort_keys=True))
PY_OBJECT
}

case "$action" in
  start)
    for candidate in "$container" "$mongodb_container" "$doris_container" "$clickhouse_container" "$minio_container"; do
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
GRANT SELECT ON public.addp_online_transfer_sql_etl_source, public.addp_wide_source, public.addp_online_transfer_failure_source, public.addp_online_transfer_invalid_json_source TO transfer_writer;
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
    start_write_failure_targets
    object_storage_fixture start
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
  object-facts)
    assert_owned "$minio_container"
    object_storage_fixture facts
    ;;
  write-failures)
    verify_write_failures
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
  manager-import)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    verify_manager_import
    ;;
  verify)
    assert_owned
    postgres_running || fail "source PostgreSQL is not running"
    assert_owned "$mongodb_container"
    postgres_sql -Atc "SELECT to_regclass('public.addp_online_transfer_config_check') IS NULL" | grep -qx 't' || fail "read-only configuration check created a target table"
    verify_fixture
    verify_write_failures >/dev/null
    echo "Online Transfer relational SQL ETL target is verified"
    ;;
  stop)
    # Check ownership before deleting either container; cleanup also covers partial startup.
    for candidate in "$container" "$mongodb_container" "$doris_container" "$clickhouse_container" "$minio_container"; do
      if docker container inspect "$candidate" >/dev/null 2>&1; then assert_owned "$candidate"; fi
    done
    cleanup_failed=0
    for candidate in "$container" "$mongodb_container" "$doris_container" "$clickhouse_container" "$minio_container"; do
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
    assert_owned "$minio_container"
    docker exec "$minio_container" curl -fsS http://127.0.0.1:9000/minio/health/ready >/dev/null || fail "upload MinIO is not ready"
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
