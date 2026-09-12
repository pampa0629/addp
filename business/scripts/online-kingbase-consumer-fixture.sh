#!/usr/bin/env bash
# Lifecycle owner for the licensed disposable KingbaseES consumer-chain T4 fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
# shellcheck source=../../scripts/lib/kingbase-official-media.sh
source "$ROOT_DIR/scripts/lib/kingbase-official-media.sh"

CONTAINER_NAME=addp-kingbase-online-disposable
DATABASE_NAME=addp_kingbase_online
DATABASE_USER=system
DATABASE_PASSWORD='AddpKingbaseV9@'
SOURCE_TABLE=addp_online_consumer_source
TARGET_TABLE=addp_online_consumer_target
WORK_DIR=${ADDP_ONLINE_SECRET_DIR:-${TMPDIR:-/tmp}}/kingbase-license-stage

fail() {
  echo "Online KingbaseES consumer fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_OWNER_MANAGED:-}" = "1" ] || fail "ADDP_ONLINE_OWNER_MANAGED must be exactly 1"
[ "${GITHUB_ACTIONS:-}" = "true" ] || fail "the fixture is restricted to GitHub Actions"
[ "${RUNNER_OS:-}" = "Linux" ] || fail "RUNNER_OS must be Linux"
[ "$(uname -s)" = "Linux" ] && [ "$(uname -m)" = "x86_64" ] ||
  fail "the official KingbaseES fixture requires Linux x86_64"

action=${1:-}
case "$action" in
  start|advance|stop|status) ;;
  *) fail "usage: bash business/scripts/online-kingbase-consumer-fixture.sh start|advance|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

container_running() {
  [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" = "true" ]
}

container_exists() {
  docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1
}

validate_container_ownership() {
  if container_exists; then
    [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$CONTAINER_NAME")" = "kingbase-consumer-flow" ] ||
      fail "$CONTAINER_NAME is not owned by the KingbaseES consumer fixture"
  fi
}

reset_fixture() {
  docker exec --interactive "$CONTAINER_NAME" "$KINGBASE_HOME/bin/ksql" -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 <<SQL >/dev/null
DROP TABLE IF EXISTS "$TARGET_TABLE";
DROP TABLE IF EXISTS "$SOURCE_TABLE";
CREATE TABLE "$SOURCE_TABLE" (
  id BIGINT PRIMARY KEY,
  item_code VARCHAR(32) NOT NULL,
  quantity INTEGER NOT NULL,
  amount NUMERIC(12, 2) NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL
);
CREATE TABLE "$TARGET_TABLE" (
  id BIGINT PRIMARY KEY,
  item_code VARCHAR(32) NOT NULL,
  quantity INTEGER NOT NULL,
  amount NUMERIC(12, 2) NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL
);
INSERT INTO "$SOURCE_TABLE" (id, item_code, quantity, amount, updated_at) VALUES
  (1, 'KB-1001', 2, 19.90, TIMESTAMP '2026-09-01 08:00:00.000001'),
  (2, 'KB-1002', 4, 39.50, TIMESTAMP '2026-09-01 08:00:00.000002'),
  (3, 'KB-1003', 1, 99.00, TIMESTAMP '2026-09-01 08:00:00.000003'),
  (4, 'KB-1004', 8, 12.25, TIMESTAMP '2026-09-01 08:00:00.000004'),
  (5, 'KB-1005', 3, 50.75, TIMESTAMP '2026-09-01 08:00:00.000005');
SQL
  kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -At -c \
    "SELECT COUNT(*) FROM \"$SOURCE_TABLE\"" | grep -Fxq '5' || fail "source fixture must contain exactly five rows"
  kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -At -c \
    "SELECT COUNT(*) FROM \"$TARGET_TABLE\"" | grep -Fxq '0' || fail "target fixture must be empty after reset"
}

advance_fixture() {
  docker exec --interactive "$CONTAINER_NAME" "$KINGBASE_HOME/bin/ksql" -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 <<SQL >/dev/null
UPDATE "$SOURCE_TABLE"
SET quantity = 5, amount = 44.50, updated_at = TIMESTAMP '2026-09-02 09:30:00.000002'
WHERE id = 2;
INSERT INTO "$SOURCE_TABLE" (id, item_code, quantity, amount, updated_at)
VALUES (6, 'KB-1006', 6, 66.60, TIMESTAMP '2026-09-02 09:30:00.000006')
ON CONFLICT (id) DO UPDATE SET
  item_code = EXCLUDED.item_code,
  quantity = EXCLUDED.quantity,
  amount = EXCLUDED.amount,
  updated_at = EXCLUDED.updated_at;
SQL
  kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d "$DATABASE_NAME" -p "$KINGBASE_DATABASE_PORT" -At -c \
    "SELECT COUNT(*) FROM \"$SOURCE_TABLE\" WHERE updated_at > TIMESTAMP '2026-09-01 08:00:00.000005'" |
    grep -Fxq '2' || fail "fixture advance must expose exactly two watermark rows"
}

validate_path() {
  local label=$1
  local path=$2
  local resolved
  [ -n "$path" ] && [ "${path#/}" != "$path" ] || fail "$label must be an absolute path"
  resolved=$(python3 - "$path" <<'PY'
import os
import sys
print(os.path.realpath(sys.argv[1]))
PY
  )
  case "$resolved" in
    "$ROOT_DIR"|"$ROOT_DIR"/*) fail "$label must be outside the repository" ;;
  esac
}

write_engine_descriptor() {
  local host_mapping host_port temporary
  validate_descriptor_path
  host_mapping=$(docker port "$CONTAINER_NAME" "$KINGBASE_DATABASE_PORT/tcp")
  host_port=${host_mapping##*:}
  [[ "$host_port" =~ ^[0-9]+$ ]] || fail "cannot resolve KingbaseES host port"
  mkdir -p "$(dirname "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE")"
  temporary="$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE.tmp"
  umask 077
  python3 - "$temporary" "$host_port" "$DATABASE_NAME" "$DATABASE_USER" "$DATABASE_PASSWORD" <<'PY'
import json
import pathlib
import sys

path, port, database, user, password = sys.argv[1:]
payload = {
    "name": "Owner-managed Online KingbaseES",
    "engine_type": "kingbase",
    "engine_origin": "general",
    "connection_info": {
        "host": "127.0.0.1",
        "port": int(port),
        "database": database,
        "user": user,
        "password": password,
        "sslmode": "disable",
    },
    "description": "Licensed disposable KingbaseES consumer-flow fixture",
}
pathlib.Path(path).write_text(json.dumps(payload, sort_keys=True) + "\n", encoding="utf-8")
PY
  chmod 600 "$temporary"
  mv "$temporary" "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
}

validate_descriptor_path() {
  validate_path "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" "${ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE:-}"
}

validate_container_ownership

case "$action" in
  start)
    validate_descriptor_path
    kingbase_validate_license_input "$ROOT_DIR"
    container_exists && fail "refusing to replace existing container: $CONTAINER_NAME"
    docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1 && fail "refusing to reuse existing image: $KINGBASE_OFFICIAL_IMAGE"
    kingbase_ensure_official_image
    mkdir -p "$WORK_DIR"
    chmod 700 "$WORK_DIR"
    docker create \
      --name "$CONTAINER_NAME" \
      --label com.addp.online-fixture=kingbase-consumer-flow \
      --publish 127.0.0.1::"$KINGBASE_DATABASE_PORT" \
      --env DB_MODE=pg \
      --env DB_USER="$DATABASE_USER" \
      --env DB_PASSWORD="$DATABASE_PASSWORD" \
      "$KINGBASE_OFFICIAL_IMAGE" >/dev/null
    kingbase_install_license_into_created_container "$CONTAINER_NAME" "$ADDP_KINGBASE_LICENSE_FILE" "$WORK_DIR"
    docker start "$CONTAINER_NAME" >/dev/null
    for _ in $(seq 1 120); do
      if container_running && kingbase_container_ready "$CONTAINER_NAME" "$DATABASE_USER"; then
        kingbase_ksql "$CONTAINER_NAME" -U "$DATABASE_USER" -d kingbase -p "$KINGBASE_DATABASE_PORT" -v ON_ERROR_STOP=1 -c \
          "CREATE DATABASE $DATABASE_NAME" >/dev/null
        reset_fixture
        write_engine_descriptor
        echo "Online KingbaseES consumer fixture is ready"
        exit 0
      fi
      container_running || fail "disposable container exited before readiness"
      sleep 5
    done
    fail "disposable container did not become ready within 600 seconds"
    ;;
  advance)
    container_running || fail "$CONTAINER_NAME is not running"
    advance_fixture
    echo "Online KingbaseES consumer fixture advanced by two watermark rows"
    ;;
  stop)
    if container_exists; then
      if container_running; then
        reset_fixture || true
      fi
      docker rm --force "$CONTAINER_NAME" >/dev/null
    fi
    if docker image inspect "$KINGBASE_OFFICIAL_IMAGE" >/dev/null 2>&1; then
      docker image rm "$KINGBASE_OFFICIAL_IMAGE" >/dev/null
    fi
    container_exists && fail "$CONTAINER_NAME still exists after cleanup"
    docker ps -a --filter "label=com.addp.online-fixture=kingbase-consumer-flow" --format '{{.Names}}' | grep -q . &&
      fail "KingbaseES Online container residual detected"
    echo "Online KingbaseES consumer fixture is stopped with zero container and image residue"
    ;;
  status)
    container_running || fail "$CONTAINER_NAME is not running"
    kingbase_container_ready "$CONTAINER_NAME" "$DATABASE_USER" || fail "fixture database is not ready"
    echo "Online KingbaseES consumer fixture is ready"
    ;;
esac
