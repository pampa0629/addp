#!/usr/bin/env bash
# Lifecycle owner for the PostgreSQL -> MySQL Transfer insert-only T4 fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)

SOURCE_TABLE=addp_online_transfer_insert_only_source
TARGET_TABLE=addp_online_transfer_insert_only_target

fail() {
  echo "Online Transfer insert-only fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online Transfer insert-only fixture requires macOS"

action=${1:-}
case "$action" in
  start|advance|verify|stop|status) ;;
  *) fail "usage: bash business/scripts/online-transfer-insert-only-fixture.sh start|advance|verify|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_TEST_ENGINE_PORT
  ADDP_ONLINE_TEST_ENGINE_USER
  ADDP_ONLINE_TEST_ENGINE_PASSWORD
  ADDP_ONLINE_TEST_ENGINE_DATABASE
  ADDP_ONLINE_TRANSFER_MYSQL_PORT
  ADDP_ONLINE_TRANSFER_MYSQL_DATABASE
  ADDP_ONLINE_TRANSFER_MYSQL_USER
  ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD
  ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done

[[ "$ADDP_ONLINE_TRANSFER_MYSQL_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_TRANSFER_MYSQL_PORT must be numeric"
[ "$ADDP_ONLINE_TRANSFER_MYSQL_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_TRANSFER_MYSQL_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_TRANSFER_MYSQL_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_TRANSFER_MYSQL_DATABASE" =~ ^[A-Za-z][A-Za-z0-9_]{0,62}$ ]] ||
  fail "ADDP_ONLINE_TRANSFER_MYSQL_DATABASE must be a safe MySQL identifier"
[[ "$ADDP_ONLINE_TRANSFER_MYSQL_USER" =~ ^[A-Za-z][A-Za-z0-9_]{0,31}$ ]] ||
  fail "ADDP_ONLINE_TRANSFER_MYSQL_USER must be a safe MySQL account name"
[[ "$ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD" =~ ^[A-Za-z0-9_.-]{16,128}$ ]] ||
  fail "ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD must contain 16-128 URL-safe characters"

docker_fixture() {
  env \
    MYSQL_PORT="$ADDP_ONLINE_TRANSFER_MYSQL_PORT" \
    MYSQL_DATABASE="$ADDP_ONLINE_TRANSFER_MYSQL_DATABASE" \
    MYSQL_ROOT_PASSWORD="$ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD" \
    docker "$@"
}

compose() {
  docker_fixture compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

postgres_running() {
  [ "$(docker inspect --format '{{.State.Running}}' business-postgres 2>/dev/null || true)" = "true" ]
}

mysql_running() {
  [ "$(docker_fixture inspect --format '{{.State.Running}}' business-mysql 2>/dev/null || true)" = "true" ]
}

postgres_sql() {
  docker exec business-postgres psql \
    -v ON_ERROR_STOP=1 \
    -U "$ADDP_ONLINE_TEST_ENGINE_USER" \
    -d "$ADDP_ONLINE_TEST_ENGINE_DATABASE" "$@"
}

root_mysql() {
  docker_fixture exec -e MYSQL_PWD="$ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD" business-mysql \
    mysql -h127.0.0.1 -uroot --default-character-set=utf8mb4 \
    "$ADDP_ONLINE_TRANSFER_MYSQL_DATABASE" "$@"
}

writer_mysql() {
  docker_fixture exec -e MYSQL_PWD="$ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD" business-mysql \
    mysql -h127.0.0.1 -u"$ADDP_ONLINE_TRANSFER_MYSQL_USER" --default-character-set=utf8mb4 \
    "$ADDP_ONLINE_TRANSFER_MYSQL_DATABASE" "$@"
}

validate_mysql_ownership() {
  if ! docker_fixture inspect business-mysql >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_fixture inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-mysql)
  [ "$ownership" = "business/mysql" ] || fail "business-mysql is not owned by the business/mysql Compose service"
}

reset_fixture() {
  postgres_sql <<SQL >/dev/null
DROP TABLE IF EXISTS public.${SOURCE_TABLE};
CREATE TABLE public.${SOURCE_TABLE} (
  id bigint PRIMARY KEY,
  name varchar(64) NOT NULL,
  points integer NOT NULL,
  amount numeric NOT NULL
);
INSERT INTO public.${SOURCE_TABLE} (id, name, points, amount) VALUES
  (1, 'customer-1', 100, 10.25),
  (2, 'customer-2', 200, 20.50),
  (3, 'customer-3', 300, 300.75),
  (4, 'customer-4', 400, 4000.00),
  (5, 'customer-5', 500, 50.05),
  (6, 'customer-6', 600, 60.60);
SQL
  root_mysql --batch --skip-column-names -e "DROP TABLE IF EXISTS \`${TARGET_TABLE}\`" >/dev/null
}

advance_fixture() {
  postgres_sql <<SQL >/dev/null
UPDATE public.${SOURCE_TABLE}
SET points = 9999, amount = 9999.99
WHERE id = 1;
INSERT INTO public.${SOURCE_TABLE} (id, name, points, amount)
VALUES (7, 'customer-7', 700, 700.70);
SQL
}

verify_fixture() {
  local values decimal_definition
  values=$(writer_mysql --batch --skip-column-names -e \
    "SELECT CONCAT_WS('|', COUNT(*), MIN(id), MAX(id), MAX(CASE WHEN id = 1 THEN points END), MAX(CASE WHEN id = 1 THEN amount END), MAX(CASE WHEN id = 7 THEN points END), MAX(CASE WHEN id = 7 THEN amount END), COUNT(*) - COUNT(DISTINCT id)) FROM \`${TARGET_TABLE}\`")
  [ "$values" = "7|1|7|100|10.25|700|700.70|0" ] ||
    fail "target rows do not prove insert-only semantics: $values"
  decimal_definition=$(writer_mysql --batch --skip-column-names -e \
    "SELECT CONCAT_WS('|', NUMERIC_PRECISION, NUMERIC_SCALE) FROM information_schema.columns WHERE table_schema = '${ADDP_ONLINE_TRANSFER_MYSQL_DATABASE}' AND table_name = '${TARGET_TABLE}' AND column_name = 'amount'")
  [ "$decimal_definition" = "6|2" ] || fail "target amount definition must be DECIMAL(6,2): $decimal_definition"
}

validate_mysql_ownership

case "$action" in
  start)
    bash "$SCRIPT_DIR/online-engine-fixture.sh" start
    compose up -d mysql
    for _ in $(seq 1 90); do
      if mysql_running && root_mysql --batch --skip-column-names -e 'SELECT 1' >/dev/null 2>&1; then
        root_mysql <<SQL >/dev/null
CREATE USER IF NOT EXISTS '${ADDP_ONLINE_TRANSFER_MYSQL_USER}'@'%' IDENTIFIED BY '${ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD}';
ALTER USER '${ADDP_ONLINE_TRANSFER_MYSQL_USER}'@'%' IDENTIFIED BY '${ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD}';
REVOKE ALL PRIVILEGES, GRANT OPTION FROM '${ADDP_ONLINE_TRANSFER_MYSQL_USER}'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX ON \`${ADDP_ONLINE_TRANSFER_MYSQL_DATABASE}\`.* TO '${ADDP_ONLINE_TRANSFER_MYSQL_USER}'@'%';
FLUSH PRIVILEGES;
SQL
        reset_fixture
        writer_mysql --batch --skip-column-names -e 'SELECT 1' | grep -qx '1' || fail "MySQL writer account is not ready"
        echo "Online Transfer insert-only fixture is ready"
        exit 0
      fi
      sleep 1
    done
    fail "business-mysql did not become ready"
    ;;
  advance)
    postgres_running || fail "business-postgres is not running"
    mysql_running || fail "business-mysql is not running"
    advance_fixture
    echo "Online Transfer insert-only source is advanced"
    ;;
  verify)
    postgres_running || fail "business-postgres is not running"
    mysql_running || fail "business-mysql is not running"
    verify_fixture
    echo "Online Transfer insert-only target is verified"
    ;;
  stop)
    if postgres_running; then
      postgres_sql -c "DROP TABLE IF EXISTS public.${SOURCE_TABLE}" >/dev/null
    fi
    if mysql_running; then
      root_mysql --batch --skip-column-names -e "DROP TABLE IF EXISTS \`${TARGET_TABLE}\`" >/dev/null
    fi
    compose rm -sf mysql
    mysql_running && fail "business-mysql is still running"
    bash "$SCRIPT_DIR/online-engine-fixture.sh" stop
    echo "Online Transfer insert-only fixture is stopped"
    ;;
  status)
    postgres_running || fail "business-postgres is not running"
    mysql_running || fail "business-mysql is not running"
    postgres_sql -Atc "SELECT COUNT(*) FROM public.${SOURCE_TABLE}" | grep -Eq '^(6|7)$' || fail "source table is not ready"
    writer_mysql --batch --skip-column-names -e 'SELECT 1' | grep -qx '1' || fail "MySQL writer account is not ready"
    echo "Online Transfer insert-only fixture is ready"
    ;;
esac
