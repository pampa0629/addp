#!/usr/bin/env bash
# Lifecycle owner for the dedicated OceanBase consumer-chain T4 Online fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)
SOURCE_TABLE=addp_online_consumer_source
TARGET_TABLE=addp_online_consumer_target
OCEANBASE_IMAGE=oceanbase/oceanbase-ce:4.4.2-lts

fail() {
  echo "Online OceanBase consumer fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online OceanBase consumer fixture requires macOS"

action=${1:-}
case "$action" in
  start|advance|stop|status) ;;
  *) fail "usage: bash business/scripts/online-oceanbase-consumer-fixture.sh start|advance|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_OCEANBASE_PORT
  ADDP_ONLINE_OCEANBASE_DATABASE
  ADDP_ONLINE_OCEANBASE_USER
  ADDP_ONLINE_OCEANBASE_PASSWORD
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done
[[ "$ADDP_ONLINE_OCEANBASE_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_OCEANBASE_PORT must be numeric"
[ "$ADDP_ONLINE_OCEANBASE_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_OCEANBASE_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_OCEANBASE_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_OCEANBASE_DATABASE" =~ ^[A-Za-z][A-Za-z0-9_]{0,62}$ ]] ||
  fail "ADDP_ONLINE_OCEANBASE_DATABASE must be a safe OceanBase identifier"
[[ "$ADDP_ONLINE_OCEANBASE_USER" =~ ^[A-Za-z][A-Za-z0-9_]{0,31}@[A-Za-z][A-Za-z0-9_]{0,31}$ ]] ||
  fail "ADDP_ONLINE_OCEANBASE_USER must be a tenant-qualified OceanBase account"
[[ "$ADDP_ONLINE_OCEANBASE_PASSWORD" =~ ^[A-Za-z0-9_.-]{16,128}$ ]] ||
  fail "ADDP_ONLINE_OCEANBASE_PASSWORD must contain 16-128 URL-safe characters"

OCEANBASE_TENANT=${ADDP_ONLINE_OCEANBASE_USER##*@}

docker_fixture() {
  env \
    OCEANBASE_IMAGE="$OCEANBASE_IMAGE" \
    OCEANBASE_MODE=mini \
    OCEANBASE_PORT="$ADDP_ONLINE_OCEANBASE_PORT" \
    OCEANBASE_DATABASE="$ADDP_ONLINE_OCEANBASE_DATABASE" \
    OCEANBASE_TENANT_NAME="$OCEANBASE_TENANT" \
    OCEANBASE_PASSWORD="$ADDP_ONLINE_OCEANBASE_PASSWORD" \
    docker "$@"
}

compose() {
  docker_fixture compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

container_running() {
  [ "$(docker_fixture inspect --format '{{.State.Running}}' business-oceanbase 2>/dev/null || true)" = "true" ]
}

admin_obclient() {
  docker_fixture exec business-oceanbase obclient \
    -h127.0.0.1 -P2881 \
    -u"$ADDP_ONLINE_OCEANBASE_USER" \
    --password="$ADDP_ONLINE_OCEANBASE_PASSWORD" "$@"
}

database_obclient() {
  admin_obclient -D"$ADDP_ONLINE_OCEANBASE_DATABASE" "$@"
}

reset_fixture() {
  admin_obclient -e "CREATE DATABASE IF NOT EXISTS \`$ADDP_ONLINE_OCEANBASE_DATABASE\`" >/dev/null
  database_obclient <<SQL >/dev/null
DROP TABLE IF EXISTS \`$TARGET_TABLE\`;
DROP TABLE IF EXISTS \`$SOURCE_TABLE\`;
CREATE TABLE \`$SOURCE_TABLE\` (
  id BIGINT NOT NULL PRIMARY KEY,
  item_code VARCHAR(32) NOT NULL,
  quantity INT NOT NULL,
  amount DECIMAL(12, 2) NOT NULL,
  updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;
CREATE TABLE \`$TARGET_TABLE\` (
  id BIGINT NOT NULL PRIMARY KEY,
  item_code VARCHAR(32) NOT NULL,
  quantity INT NOT NULL,
  amount DECIMAL(12, 2) NOT NULL,
  updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB;
INSERT INTO \`$SOURCE_TABLE\` (id, item_code, quantity, amount, updated_at) VALUES
  (1, 'OB-1001', 2, 19.90, '2026-09-01 08:00:00.000001'),
  (2, 'OB-1002', 4, 39.50, '2026-09-01 08:00:00.000002'),
  (3, 'OB-1003', 1, 99.00, '2026-09-01 08:00:00.000003'),
  (4, 'OB-1004', 8, 12.25, '2026-09-01 08:00:00.000004'),
  (5, 'OB-1005', 3, 50.75, '2026-09-01 08:00:00.000005');
SQL
  database_obclient --batch --skip-column-names -e \
    "SELECT COUNT(*) FROM \`$SOURCE_TABLE\`" | grep -qx '5' ||
    fail "OceanBase source fixture must contain exactly five rows"
  database_obclient --batch --skip-column-names -e \
    "SELECT COUNT(*) FROM \`$TARGET_TABLE\`" | grep -qx '0' ||
    fail "OceanBase target fixture must be empty after reset"
}

advance_fixture() {
  database_obclient <<SQL >/dev/null
UPDATE \`$SOURCE_TABLE\`
SET quantity = 5, amount = 44.50, updated_at = '2026-09-02 09:30:00.000002'
WHERE id = 2;
INSERT INTO \`$SOURCE_TABLE\` (id, item_code, quantity, amount, updated_at)
VALUES (6, 'OB-1006', 6, 66.60, '2026-09-02 09:30:00.000006')
ON DUPLICATE KEY UPDATE
  item_code = VALUES(item_code),
  quantity = VALUES(quantity),
  amount = VALUES(amount),
  updated_at = VALUES(updated_at);
SQL
  database_obclient --batch --skip-column-names -e \
    "SELECT COUNT(*) FROM \`$SOURCE_TABLE\` WHERE updated_at > '2026-09-01 08:00:00.000005'" |
    grep -qx '2' || fail "OceanBase fixture advance must expose exactly two watermark rows"
}

validate_container_ownership() {
  if ! docker_fixture inspect business-oceanbase >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_fixture inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-oceanbase)
  [ "$ownership" = "business/oceanbase" ] ||
    fail "business-oceanbase is not owned by the business/oceanbase Compose service"
}

validate_container_ownership

case "$action" in
  start)
    compose up -d oceanbase
    for _ in $(seq 1 180); do
      if container_running && admin_obclient --batch --skip-column-names -e 'SELECT 1' >/dev/null 2>&1; then
        reset_fixture
        echo "Online OceanBase consumer fixture is ready on port $ADDP_ONLINE_OCEANBASE_PORT"
        exit 0
      fi
      sleep 2
    done
    fail "business-oceanbase did not become ready"
    ;;
  advance)
    container_running || fail "business-oceanbase is not running"
    advance_fixture
    echo "Online OceanBase consumer fixture advanced by two watermark rows"
    ;;
  stop)
    if container_running; then
      reset_fixture
    fi
    compose rm -sf oceanbase
    if container_running; then
      fail "business-oceanbase is still running"
    fi
    echo "Online OceanBase consumer fixture is stopped"
    ;;
  status)
    container_running || fail "business-oceanbase is not running"
    database_obclient --batch --skip-column-names -e 'SELECT 1' | grep -qx '1' ||
      fail "business-oceanbase fixture account is not ready"
    echo "Online OceanBase consumer fixture is ready"
    ;;
esac
