#!/usr/bin/env bash
# Lifecycle owner for the dedicated read-only MySQL fixture used by Workbench T4 Online acceptance.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)

fail() {
  echo "Online Workbench MySQL fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online Workbench MySQL fixture requires macOS"

action=${1:-}
case "$action" in
  start|stop|status) ;;
  *) fail "usage: bash business/scripts/online-workbench-mysql-fixture.sh start|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_WORKBENCH_MYSQL_PORT
  ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE
  ADDP_ONLINE_WORKBENCH_MYSQL_USER
  ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD
  ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done
[[ "$ADDP_ONLINE_WORKBENCH_MYSQL_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_WORKBENCH_MYSQL_PORT must be numeric"
[ "$ADDP_ONLINE_WORKBENCH_MYSQL_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_WORKBENCH_MYSQL_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_WORKBENCH_MYSQL_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE" =~ ^[A-Za-z][A-Za-z0-9_]{0,62}$ ]] ||
  fail "ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE must be a safe MySQL identifier"
[[ "$ADDP_ONLINE_WORKBENCH_MYSQL_USER" =~ ^[A-Za-z][A-Za-z0-9_]{0,31}$ ]] ||
  fail "ADDP_ONLINE_WORKBENCH_MYSQL_USER must be a safe MySQL account name"
[[ "$ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD" =~ ^[A-Za-z0-9_.-]{16,128}$ ]] ||
  fail "ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD must contain 16-128 URL-safe characters"

docker_fixture() {
  env \
    MYSQL_PORT="$ADDP_ONLINE_WORKBENCH_MYSQL_PORT" \
    MYSQL_DATABASE="$ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE" \
    MYSQL_ROOT_PASSWORD="$ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD" \
    docker "$@"
}

compose() {
  docker_fixture compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

container_running() {
  [ "$(docker_fixture inspect --format '{{.State.Running}}' business-mysql 2>/dev/null || true)" = "true" ]
}

root_mysql() {
  docker_fixture exec -e MYSQL_PWD="$ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD" business-mysql \
    mysql -h127.0.0.1 -uroot --default-character-set=utf8mb4 "$ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE" "$@"
}

reader_mysql() {
  docker_fixture exec -e MYSQL_PWD="$ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD" business-mysql \
    mysql -h127.0.0.1 -u"$ADDP_ONLINE_WORKBENCH_MYSQL_USER" --default-character-set=utf8mb4 \
    "$ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE" "$@"
}

seed_fixture() {
  env \
    MYSQL_CONTAINER=business-mysql \
    MYSQL_USER=root \
    MYSQL_ROOT_PASSWORD="$ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD" \
    MYSQL_DATABASE="$ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE" \
    bash "$BUSINESS_DIR/mysql/test-data.sh" >/dev/null

  root_mysql <<SQL >/dev/null
CREATE USER IF NOT EXISTS '${ADDP_ONLINE_WORKBENCH_MYSQL_USER}'@'%' IDENTIFIED BY '${ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD}';
ALTER USER '${ADDP_ONLINE_WORKBENCH_MYSQL_USER}'@'%' IDENTIFIED BY '${ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD}';
REVOKE ALL PRIVILEGES, GRANT OPTION FROM '${ADDP_ONLINE_WORKBENCH_MYSQL_USER}'@'%';
GRANT SELECT ON \`${ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE}\`.* TO '${ADDP_ONLINE_WORKBENCH_MYSQL_USER}'@'%';
FLUSH PRIVILEGES;
SQL
  reader_mysql --batch --skip-column-names -e \
    'SELECT COUNT(*) FROM orders o JOIN customers c ON c.id = o.customer_id' | grep -qx '4' ||
    fail "commerce fixture must contain exactly four joined orders"
}

validate_container_ownership() {
  if ! docker_fixture inspect business-mysql >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_fixture inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-mysql)
  [ "$ownership" = "business/mysql" ] ||
    fail "business-mysql is not owned by the business/mysql Compose service"
}

validate_container_ownership

case "$action" in
  start)
    compose up -d mysql
    for _ in $(seq 1 90); do
      if container_running && root_mysql --batch --skip-column-names -e 'SELECT 1' >/dev/null 2>&1; then
        seed_fixture
        echo "Online Workbench MySQL fixture is ready on port $ADDP_ONLINE_WORKBENCH_MYSQL_PORT"
        exit 0
      fi
      sleep 1
    done
    fail "business-mysql did not become ready"
    ;;
  stop)
    compose rm -sf mysql
    if container_running; then
      fail "business-mysql is still running"
    fi
    echo "Online Workbench MySQL fixture is stopped"
    ;;
  status)
    container_running || fail "business-mysql is not running"
    reader_mysql --batch --skip-column-names -e 'SELECT 1' | grep -qx '1' ||
      fail "business-mysql read-only account is not ready"
    echo "Online Workbench MySQL fixture is ready"
    ;;
esac
