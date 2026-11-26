#!/usr/bin/env bash
# Lifecycle owner for the Hosted TiDB consumer-chain T4 Online fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.tidb-t2.yml"
MYSQL_CLIENT_IMAGE=mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b
SOURCE_TABLE=addp_online_consumer_source
TARGET_TABLE=addp_online_consumer_target

fail() {
  echo "Online TiDB consumer fixture failed: $*" >&2
  exit 1
}

ADDP_ONLINE_TIDB_DATABASE=tidb_fixture
compose() {
  TIDB_T2_PORT="$ADDP_ONLINE_TIDB_PORT" \
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"
}

cluster_running() {
  compose ps --status running --services 2>/dev/null | grep -Fxq tidb
}

tidb_mysql() {
  docker run --rm -i --network "${COMPOSE_PROJECT}_default" \
    "$MYSQL_CLIENT_IMAGE" mysql \
    -htidb -P4000 -uroot \
    --protocol=tcp --connect-timeout=5 --default-character-set=utf8mb4 "$@"
}

database_mysql() {
  tidb_mysql -D"$ADDP_ONLINE_TIDB_DATABASE" "$@"
}

reset_fixture() {
  local password
  password=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
  # TiDB's root session provisions this disposable fixture. Only the limited
  # source account is registered with System.
  tidb_mysql >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/tidb-seed-error.log" <<SQL
CREATE DATABASE tidb_fixture;
CREATE DATABASE tidb_scope;
CREATE DATABASE tidb_outside;
CREATE TABLE tidb_scope.node_existing (id BIGINT PRIMARY KEY);
INSERT INTO tidb_scope.node_existing VALUES (37);
CREATE USER 'tidb_consumer'@'%' IDENTIFIED BY '$password';
GRANT SELECT ON tidb_scope.* TO 'tidb_consumer'@'%';
GRANT SELECT ON tidb_outside.* TO 'tidb_consumer'@'%';
SQL
  database_mysql <<SQL >/dev/null
DROP TABLE IF EXISTS \`$TARGET_TABLE\`;
DROP TABLE IF EXISTS \`$SOURCE_TABLE\`;
CREATE TABLE \`$SOURCE_TABLE\` (
  id BIGINT NOT NULL PRIMARY KEY,
  item_code VARCHAR(32) NOT NULL,
  quantity INT NOT NULL,
  amount DECIMAL(12, 2) NOT NULL,
  updated_at DATETIME(6) NOT NULL
);
CREATE TABLE \`$TARGET_TABLE\` (
  id BIGINT NOT NULL PRIMARY KEY,
  item_code VARCHAR(32) NOT NULL,
  quantity INT NOT NULL,
  amount DECIMAL(12, 2) NOT NULL,
  updated_at DATETIME(6) NOT NULL
);
INSERT INTO \`$SOURCE_TABLE\` (id, item_code, quantity, amount, updated_at) VALUES
  (1, 'TIDB-1001', 2, 19.90, '2026-09-01 08:00:00.000001'),
  (2, 'TIDB-1002', 4, 39.50, '2026-09-01 08:00:00.000002'),
  (3, 'TIDB-1003', 1, 99.00, '2026-09-01 08:00:00.000003'),
  (4, 'TIDB-1004', 8, 12.25, '2026-09-01 08:00:00.000004'),
  (5, 'TIDB-1005', 3, 50.75, '2026-09-01 08:00:00.000005');
SQL
  tidb_mysql <<SQL
GRANT SELECT ON tidb_fixture.$SOURCE_TABLE TO 'tidb_consumer'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE ON tidb_fixture.$TARGET_TABLE TO 'tidb_consumer'@'%';
SQL
  TIDB_SOURCE_PORT="$ADDP_ONLINE_TIDB_PORT" TIDB_SOURCE_PASSWORD="$password" python3 - <<'DESCRIPTOR'
import json, os
descriptor = {"engine_type":"tidb","name":"Hosted TiDB consumer","engine_origin":"general","description":"Isolated TiDB consumer and node grant acceptance",
              "connection_info":{"host":"127.0.0.1","port":int(os.environ["TIDB_SOURCE_PORT"]),"database":"tidb_fixture",
                                 "user":"tidb_consumer","password":os.environ["TIDB_SOURCE_PASSWORD"]}}
fd = os.open(os.environ["ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"], os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
with os.fdopen(fd, "w") as stream:
    json.dump(descriptor, stream)
DESCRIPTOR
  database_mysql --batch --skip-column-names -e \
    "SELECT COUNT(*) FROM \`$SOURCE_TABLE\`" | grep -qx '5' ||
    fail "TiDB source fixture must contain exactly five rows"
  database_mysql --batch --skip-column-names -e \
    "SELECT COUNT(*) FROM \`$TARGET_TABLE\`" | grep -qx '0' ||
    fail "TiDB target fixture must be empty after reset"
}

advance_fixture() {
  database_mysql <<SQL >/dev/null
UPDATE \`$SOURCE_TABLE\`
SET quantity = 5, amount = 44.50, updated_at = '2026-09-02 09:30:00.000002'
WHERE id = 2;
INSERT INTO \`$SOURCE_TABLE\` (id, item_code, quantity, amount, updated_at)
VALUES (6, 'TIDB-1006', 6, 66.60, '2026-09-02 09:30:00.000006')
ON DUPLICATE KEY UPDATE
  item_code = VALUES(item_code),
  quantity = VALUES(quantity),
  amount = VALUES(amount),
  updated_at = VALUES(updated_at);
SQL
  database_mysql --batch --skip-column-names -e \
    "SELECT COUNT(*) FROM \`$SOURCE_TABLE\` WHERE updated_at > '2026-09-01 08:00:00.000005'" |
    grep -qx '2' || fail "TiDB fixture advance must expose exactly two watermark rows"
}

assert_zero_residue() {
  local kind ids
  for kind in container network volume; do
    if [ "$kind" = container ]; then
      ids=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT")
    else
      ids=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT")
    fi
    [ -z "$ids" ] || fail "TiDB fixture left $kind resources"
  done
}

create_future() {
    tidb_mysql <<'SQL'
CREATE TABLE tidb_scope.node_future (id BIGINT PRIMARY KEY);
INSERT INTO tidb_scope.node_future VALUES (41);
CREATE TABLE tidb_outside.node_future (id BIGINT PRIMARY KEY);
INSERT INTO tidb_outside.node_future VALUES (83);
SQL
}

count_statements() {
    # Deduplicate current and historical summary windows by their native
    # identity. Only the three literal zero-row identity probes are exempt.
    tidb_mysql --batch --skip-column-names <<'SQL'
SELECT COALESCE(SUM(IF(probe,0,n)),0),COALESCE(SUM(IF(probe,n,0)),0)
FROM (
  SELECT summary_begin_time, schema_name, digest, plan_digest, MAX(exec_count) AS n,
    LOWER(TRIM(query_sample_text)) IN (
      'select 1 from `tidb_scope`.`node_existing` limit 0',
      'select 1 from `tidb_scope`.`node_future` limit 0',
      'select 1 from `tidb_outside`.`node_future` limit 0') AS probe
  FROM (
    SELECT summary_begin_time,schema_name,digest,plan_digest,exec_count,query_sample_text
    FROM information_schema.statements_summary
    WHERE sample_user='tidb_consumer' AND stmt_type='Select'
      AND (LOCATE('`node_existing`',digest_text)>0 OR LOCATE('`node_future`',digest_text)>0)
    UNION ALL
    SELECT summary_begin_time,schema_name,digest,plan_digest,exec_count,query_sample_text
    FROM information_schema.statements_summary_history
    WHERE sample_user='tidb_consumer' AND stmt_type='Select'
      AND (LOCATE('`node_existing`',digest_text)>0 OR LOCATE('`node_future`',digest_text)>0)
  ) AS windows_by_digest
  GROUP BY summary_begin_time,schema_name,digest,plan_digest,probe
) AS observed;
SQL
}

# The native gate sources the same fixture functions for its own Compose project.
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
  return 0
fi
COMPOSE_PROJECT=addp-online-tidb-consumer
[ "${ADDP_ONLINE_HOST:-}/${ADDP_ONLINE_HOSTED:-}/${GITHUB_ACTIONS:-}/${RUNNER_OS:-}" = "1/1/true/Linux" ] ||
  fail "requires the disposable GitHub Hosted Linux environment"
[ "$(uname -s)" = Linux ] || fail "requires Linux"
[ "${ONLINE_SUITE_INPUT:-}" = tidb-consumer-flow ] || fail "unexpected Online suite"
action=${1:-}
case "$action" in
  start|advance|stop|status|create-future|counts) ;;
  *) fail "expected start, advance, stop, status, create-future or counts" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"
ADDP_ONLINE_TIDB_PORT=55434
case "$action" in
  start)
    compose up -d --force-recreate tidb-pd tidb-tikv tidb
    for _ in $(seq 1 180); do
      if cluster_running && tidb_mysql --batch --skip-column-names -e 'SELECT 1' >/dev/null 2>&1; then
        reset_fixture
        echo "Online TiDB consumer fixture is ready on port $ADDP_ONLINE_TIDB_PORT"
        exit 0
      fi
      sleep 2
    done
    fail "TiDB cluster did not become ready"
    ;;
  create-future) create_future ;;
  counts) count_statements ;;
  advance)
    cluster_running || fail "TiDB cluster is not running"
    advance_fixture
    echo "Online TiDB consumer fixture advanced by two watermark rows"
    ;;
  stop)
    compose down --volumes --remove-orphans
    assert_zero_residue
    echo "Online TiDB consumer fixture is stopped with zero container residue"
    ;;
  status)
    cluster_running || fail "TiDB cluster is not running"
    database_mysql --batch --skip-column-names -e 'SELECT 1' | grep -qx '1' ||
      fail "TiDB fixture account is not ready"
    echo "Online TiDB consumer fixture is ready"
    ;;
esac
