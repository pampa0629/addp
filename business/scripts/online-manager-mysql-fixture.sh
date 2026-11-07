#!/usr/bin/env bash
# Read-only spatial source owned by the isolated Manager FileGDB MySQL case.
set -euo pipefail
umask 077
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
fail() { echo "Manager MySQL fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "${POSTGRES_DB:-}" = addp_online ] && [ "${ADDP_ONLINE_MANAGER_CASE:-}" = filegdb-mysql-export ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] ||
  fail 'the exclusive Hosted MySQL FileGDB case is required'
[ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] && [ -n "${ADDP_ONLINE_TEST_RUN_ID:-}" ] || fail 'private deployment context missing'
container=addp-manager-mysql-online-disposable
owner=manager-filegdb-mysql
assert_owned() {
  [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = "$owner" ] &&
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-run" }}' "$container")" = "$ADDP_ONLINE_TEST_RUN_ID" ] ||
    fail 'container belongs to another deployment'
}
root_sql() {
  MYSQL_PWD="$MYSQL_ROOT_PASSWORD" docker exec -i -e MYSQL_PWD "$container" \
    mysql -uroot --default-character-set=utf8mb4 --batch --skip-column-names "$@"
}
case "${1:-}" in
  stop)
    [ "$#" = 1 ] || fail 'stop takes no table'
    if docker container inspect "$container" >/dev/null 2>&1; then
      assert_owned
      docker rm -fv "$container" >/dev/null
    fi
    ! docker container inspect "$container" >/dev/null 2>&1 || fail 'source container remains after cleanup'
    exit 0 ;;
  snapshot-filegdb)
    [ "$#" = 2 ] && [ "${ADDP_ONLINE_TEST:-}" = 1 ] || fail 'snapshot requires the active test and one exact table'
    case "$2" in filegdb_export_points|filegdb_export_lines|filegdb_export_polygons) ;; *) fail 'unknown source table' ;; esac
    assert_owned
    MYSQL_PWD=$(python3 -c 'import json,os; print(json.load(open(os.environ["ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"]))["connection_info"]["password"])')
    export MYSQL_PWD
    docker exec -i -e MYSQL_PWD "$container" mysql -ufilegdb_reader --default-character-set=utf8mb4 \
      --batch --skip-column-names filegdb_fixture <<SQL
SET SESSION group_concat_max_len=1048576;
START TRANSACTION READ ONLY;
SELECT JSON_OBJECT(
 'rows', (SELECT MD5(GROUP_CONCAT(JSON_ARRAY(seq,HEX(\`标题\`),flag,measure,
   HEX(ST_AsWKB(\`shape\`, 'axis-order=long-lat')),ST_SRID(\`shape\`)) ORDER BY seq SEPARATOR '')) FROM \`$2\`),
 'count', (SELECT COUNT(*) FROM \`$2\`),
 'columns', (SELECT MD5(GROUP_CONCAT(JSON_ARRAY(COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,SRS_ID) ORDER BY ORDINAL_POSITION SEPARATOR ''))
   FROM information_schema.columns WHERE table_schema='filegdb_fixture' AND table_name='$2'));
COMMIT;
SQL
    exit 0 ;;
  start) [ "$#" = 1 ] || fail 'start takes no table' ;;
  *) fail 'expected start, stop or snapshot-filegdb TABLE' ;;
esac
! docker container inspect "$container" >/dev/null 2>&1 || fail 'refusing to reuse an existing source container'
MYSQL_ROOT_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MYSQL_READER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
export MYSQL_ROOT_PASSWORD MYSQL_READER_PASSWORD
image=$(docker compose --env-file /dev/null -f "$ROOT_DIR/scripts/test/docker-compose.local-macos-ci.yml" config --format json |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["mysql"]["image"])')
docker run -d --name "$container" --label "com.addp.online-fixture=$owner" \
  --label "com.addp.online-run=$ADDP_ONLINE_TEST_RUN_ID" --tmpfs /var/lib/mysql \
  -p 127.0.0.1::3306 -e MYSQL_ROOT_PASSWORD "$image" \
  --character-set-server=utf8mb4 --collation-server=utf8mb4_bin >/dev/null
ready=0
for _ in $(seq 1 90); do
  if root_sql -e 'SELECT 1' > /dev/null 2>"$ADDP_ONLINE_SECRET_DIR/mysql-readiness.log"; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || fail 'source MySQL did not become ready'
if ! root_sql > /dev/null 2>"$ADDP_ONLINE_SECRET_DIR/mysql-seed.log" <<SQL
CREATE DATABASE filegdb_fixture CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
USE filegdb_fixture;
CREATE TABLE filegdb_export_points (seq INTEGER PRIMARY KEY, \`标题\` TEXT, flag BOOLEAN, measure DOUBLE, shape POINT SRID 3857 NULL) ENGINE=InnoDB;
CREATE TABLE filegdb_export_lines (seq INTEGER PRIMARY KEY, \`标题\` TEXT, flag BOOLEAN, measure DOUBLE, shape MULTILINESTRING SRID 4326 NULL) ENGINE=InnoDB;
CREATE TABLE filegdb_export_polygons (seq INTEGER PRIMARY KEY, \`标题\` TEXT, flag BOOLEAN, measure DOUBLE, shape MULTIPOLYGON SRID 4326 NULL) ENGINE=InnoDB;
INSERT INTO filegdb_export_points VALUES (1,'长沙',1,1.25,ST_GeomFromText('POINT (12000 6000)',3857)),(2,'',0,2.5,ST_GeomFromText('POINT (-8000 2300)',3857)),(3,NULL,1,NULL,NULL);
INSERT INTO filegdb_export_lines VALUES
 (1,'长沙',1,1.25,ST_GeomFromText('MULTILINESTRING ((110 28,111 29,112 29.25),(114 30,115 30.5))',4326,'axis-order=long-lat')),
 (2,'',0,2.5,ST_GeomFromText('MULTILINESTRING ((-121 45,-120.25 45.5))',4326,'axis-order=long-lat')),(3,NULL,1,NULL,NULL);
INSERT INTO filegdb_export_polygons VALUES
 (1,'长沙',1,1.25,ST_GeomFromText('MULTIPOLYGON (((110 28,114 28,114 32,110 32,110 28),(111 29,111 30,112 30,112 29,111 29)),((115 28,117 28,117 29,115 28)))',4326,'axis-order=long-lat')),
 (2,'',0,2.5,ST_GeomFromText('MULTIPOLYGON (((-121 44,-120 44,-120 45,-121 45,-121 44)))',4326,'axis-order=long-lat')),(3,NULL,1,NULL,NULL);
CREATE USER filegdb_reader IDENTIFIED BY '$MYSQL_READER_PASSWORD';
GRANT SELECT ON filegdb_fixture.filegdb_export_points TO filegdb_reader;
GRANT SELECT ON filegdb_fixture.filegdb_export_lines TO filegdb_reader;
GRANT SELECT ON filegdb_fixture.filegdb_export_polygons TO filegdb_reader;
SQL
then fail 'spatial seed failed; private diagnostics retained'; fi
binding=$(docker inspect -f '{{with index (index .NetworkSettings.Ports "3306/tcp") 0}}{{.HostIp}}:{{.HostPort}}{{end}}' "$container")
[[ "$binding" =~ ^127\.0\.0\.1:([0-9]+)$ ]] || fail 'MySQL source must have an assigned loopback port'
export MANAGER_MYSQL_FIXTURE_PORT="${BASH_REMATCH[1]}"
python3 - <<'PY'
import json,os
port=int(os.environ['MANAGER_MYSQL_FIXTURE_PORT']);assert 0 < port < 65536
with os.fdopen(os.open(os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE'],os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600),'w') as out:
 json.dump({'name':'Hosted Manager MySQL','engine_type':'mysql','engine_origin':'general',
  'description':'Disposable read-only FileGDB spatial source','connection_info':{'host':'127.0.0.1','port':port,
  'database':'filegdb_fixture','user':'filegdb_reader','password':os.environ['MYSQL_READER_PASSWORD']}},out)
PY
unset MYSQL_ROOT_PASSWORD MYSQL_READER_PASSWORD
echo 'Manager MySQL spatial fixture ready'
