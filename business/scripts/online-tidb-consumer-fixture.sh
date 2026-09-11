#!/usr/bin/env bash
# Lifecycle owner for the dedicated TiDB consumer-chain T4 Online fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.tidb-t2.yml"
COMPOSE_PROJECT=addp-online-tidb-consumer
MYSQL_CLIENT_IMAGE=mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b
SOURCE_TABLE=addp_online_consumer_source
TARGET_TABLE=addp_online_consumer_target

fail() {
  echo "Online TiDB consumer fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online TiDB consumer fixture requires macOS"

action=${1:-}
case "$action" in
  start|advance|stop|status) ;;
  *) fail "usage: bash business/scripts/online-tidb-consumer-fixture.sh start|advance|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_TIDB_PORT
  ADDP_ONLINE_TIDB_DATABASE
  ADDP_ONLINE_TIDB_USER
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done
[[ "$ADDP_ONLINE_TIDB_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_TIDB_PORT must be numeric"
[ "$ADDP_ONLINE_TIDB_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_TIDB_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_TIDB_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_TIDB_DATABASE" =~ ^[A-Za-z][A-Za-z0-9_]{0,63}$ ]] ||
  fail "ADDP_ONLINE_TIDB_DATABASE must be a safe TiDB identifier"
[[ "$ADDP_ONLINE_TIDB_USER" =~ ^[A-Za-z][A-Za-z0-9_.-]{0,63}$ ]] ||
  fail "ADDP_ONLINE_TIDB_USER must be a safe TiDB account"

compose() {
  TIDB_T2_PORT="$ADDP_ONLINE_TIDB_PORT" \
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"
}

cluster_running() {
  compose ps --status running --services 2>/dev/null | grep -Fxq tidb
}

tidb_mysql() {
  docker run --rm --network "${COMPOSE_PROJECT}_default" \
    -e "MYSQL_PWD=${ADDP_ONLINE_TIDB_PASSWORD:-}" \
    "$MYSQL_CLIENT_IMAGE" mysql \
    -htidb -P4000 -u"$ADDP_ONLINE_TIDB_USER" \
    --protocol=tcp --connect-timeout=5 --default-character-set=utf8mb4 "$@"
}

database_mysql() {
  tidb_mysql -D"$ADDP_ONLINE_TIDB_DATABASE" "$@"
}

reset_fixture() {
  tidb_mysql -e "CREATE DATABASE IF NOT EXISTS \`$ADDP_ONLINE_TIDB_DATABASE\` CHARACTER SET utf8mb4" >/dev/null
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
  if docker ps -a \
    --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" \
    --format '{{.ID}}' | grep -q .; then
    fail "TiDB fixture cleanup left Compose containers"
  fi
}

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
