#!/usr/bin/env bash
# Disposable OceanBase source for the Hosted relational consumer suite.
set -euo pipefail
SOURCE_TABLE=addp_online_consumer_source
TARGET_TABLE=addp_online_consumer_target
IMAGE=oceanbase/oceanbase-ce:4.4.2-lts@sha256:eaa2c3b3513fa21e4a658f197c1deb93c9d6b6eb742cfa5ef0f085f82b9cec54

fail() { echo "OceanBase fixture: $*" >&2; exit 1; }

admin_obclient() {
  docker exec -i "$CONTAINER" sh -c 'exec obclient -h127.0.0.1 -P2881 -uroot@test --password="$OB_TENANT_PASSWORD" "$@"' _ "$@"
}

database_obclient() { admin_obclient -Doceanbase_fixture "$@"; }

reset_fixture() {
  local password
  password=$(openssl rand -hex 32)
  admin_obclient > /dev/null 2>"$ADDP_ONLINE_SECRET_DIR/ob-seed-error.log" <<SQL
CREATE DATABASE oceanbase_fixture;
CREATE DATABASE sales;
CREATE DATABASE outside_sales;
CREATE TABLE sales.node_existing (id INT PRIMARY KEY);
INSERT INTO sales.node_existing VALUES (37);
CREATE USER ob_consumer IDENTIFIED BY '$password';
GRANT SELECT ON sales.* TO ob_consumer;
GRANT SELECT ON outside_sales.* TO ob_consumer;
SQL
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
  admin_obclient <<SQL
GRANT SELECT ON oceanbase_fixture.$SOURCE_TABLE TO ob_consumer;
GRANT SELECT, INSERT, UPDATE, DELETE ON oceanbase_fixture.$TARGET_TABLE TO ob_consumer;
SQL
  # Ordinary tenants hide QUERY_SQL by default. Only the disposable source
  # tenant enables observation, after credential provisioning has finished.
  admin_obclient -e 'ALTER SYSTEM SET _enable_sql_audit_query_sql = true'
  OB_PORT=$(docker port "$CONTAINER" 2881/tcp | cut -d: -f2) OB_PASSWORD="$password" python3 - <<'DESCRIPTOR'
import json, os
connection = dict(host="127.0.0.1", port=int(os.environ["OB_PORT"]), database="oceanbase_fixture",
                  user="ob_consumer@test", password=os.environ["OB_PASSWORD"])
with open(os.open(os.environ["ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"],
                  os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), "w") as descriptor:
    json.dump(dict(engine_type="oceanbase", engine_origin="general", name="Hosted OceanBase consumer",
                   description="Disposable OceanBase relational consumer", connection_info=connection), descriptor)
DESCRIPTOR
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

create_future() {
  admin_obclient <<'SQL'
CREATE TABLE sales.node_future (id INT PRIMARY KEY);
INSERT INTO sales.node_future VALUES (41);
CREATE TABLE outside_sales.node_future (id INT PRIMARY KEY);
INSERT INTO outside_sales.node_future VALUES (83);
SQL
}

audit_position() {
  admin_obclient -N -B -Doceanbase -e 'SELECT MAX(REQUEST_ID) FROM GV$OB_SQL_AUDIT'
}

count_reads() {
  local anchor=$1
  [[ "$anchor" =~ ^[0-9]+$ ]] || fail "missing native audit position"
  # A retained anchor on this single observer proves the observation window
  # has not been evicted. Report no count if it has.
  admin_obclient -N -B -Doceanbase <<SQL
SELECT COUNT(*) FROM GV\$OB_SQL_AUDIT
WHERE REQUEST_ID > $anchor AND USER_NAME='ob_consumer' AND IS_INNER_SQL=0
  AND LOWER(TRIM(QUERY_SQL)) LIKE 'select%'
  AND LOWER(REPLACE(QUERY_SQL,CHAR(96),'')) REGEXP 'from[[:space:]]+((outside_sales|sales)[.])?node_(existing|future)([[:space:];]|$)'
HAVING EXISTS (SELECT 1 FROM GV\$OB_SQL_AUDIT WHERE REQUEST_ID=$anchor);
SQL
}

if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return 0; fi
[ "${ADDP_ONLINE_HOST:-}/${ADDP_ONLINE_HOSTED:-}/${GITHUB_ACTIONS:-}/${RUNNER_OS:-}" = "1/1/true/Linux" ] &&
  [ "$(uname -s)" = Linux ] && [ "${ONLINE_SUITE_INPUT:-}" = oceanbase-consumer-flow ] ||
  fail "requires the disposable GitHub Hosted Linux suite"
CONTAINER=addp-online-oceanbase-consumer
owner="oceanbase-consumer-flow:${GITHUB_RUN_ID:?}:${GITHUB_RUN_ATTEMPT:?}"
[ "$#" = 1 ] || fail "expected one action"
if docker container inspect "$CONTAINER" >/dev/null 2>&1; then
  [ "$(docker inspect --format '{{index .Config.Labels "addp.fixture"}}' "$CONTAINER")" = "$owner" ] ||
    fail "refusing foreign container"
fi
case "$1" in
  start)
    OB_TENANT_PASSWORD=$(openssl rand -hex 32)
    export OB_TENANT_PASSWORD
    docker run -d --name "$CONTAINER" --label "addp.fixture=$owner" -p 127.0.0.1::2881 \
      -e MODE=mini -e OB_TENANT_NAME=test -e OB_DATABASE=oceanbase -e OB_TENANT_PASSWORD "$IMAGE"
    for _ in $(seq 1 180); do
      if admin_obclient -Nse 'SELECT 1' >/dev/null 2>&1; then reset_fixture; exit 0; fi
      sleep 2
    done
    fail "source did not become ready"
    ;;
  status) admin_obclient -Nse 'SELECT 1' | grep -qx 1 ;;
  advance) advance_fixture ;;
  create-future) create_future ;;
  position) audit_position ;;
  counts) count_reads "${OB_AUDIT_POSITION:?}" ;;
  stop)
    if docker container inspect "$CONTAINER" >/dev/null 2>&1; then
      volumes=$(docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{println .Name}}{{end}}{{end}}' "$CONTAINER")
      docker rm -fv "$CONTAINER"
      for volume in $volumes; do
        [ -z "$(docker volume ls -q --filter "name=^$volume$")" ] || fail "source volume remains"
      done
    fi
    remaining=$(docker ps -aq --filter "label=addp.fixture=$owner")
    [ -z "$remaining" ] || fail "source container remains"
    ;;
  *) fail "unknown action" ;;
esac
