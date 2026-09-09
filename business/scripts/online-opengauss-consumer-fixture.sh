#!/usr/bin/env bash
# Lifecycle owner for the disposable Linux x86_64 openGauss consumer-chain T4 fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
# shellcheck source=../../scripts/lib/opengauss-official-media.sh
source "$ROOT_DIR/scripts/lib/opengauss-official-media.sh"

CONTAINER_NAME=addp-opengauss-online-disposable
DATABASE_NAME=addp_opengauss_online
DATABASE_USER=gaussdb
DATABASE_PASSWORD='AddpGauss606@'
SOURCE_TABLE=addp_online_consumer_source
TARGET_TABLE=addp_online_consumer_target
OPENGAUSS_HOME=/usr/local/opengauss
OPENGAUSS_EXEC_PATH=/usr/local/opengauss/bin:/scws/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
OPENGAUSS_LIBRARY_PATH=/usr/local/opengauss/lib:/scws/lib

fail() {
  echo "Online openGauss consumer fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOSTED:-}" = "1" ] || fail "ADDP_ONLINE_HOSTED must be exactly 1"
[ "${GITHUB_ACTIONS:-}" = "true" ] || fail "the fixture is restricted to GitHub Actions"
[ "${RUNNER_OS:-}" = "Linux" ] || fail "RUNNER_OS must be Linux"
[ "$(uname -s)" = "Linux" ] && [ "$(uname -m)" = "x86_64" ] ||
  fail "the official openGauss fixture requires Linux x86_64"

action=${1:-}
case "$action" in
  start|advance|stop|status) ;;
  *) fail "usage: bash business/scripts/online-opengauss-consumer-fixture.sh start|advance|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

opengauss_gsql() {
  docker exec --interactive --user omm \
    --env "GAUSSHOME=$OPENGAUSS_HOME" \
    --env "PATH=$OPENGAUSS_EXEC_PATH" \
    --env "LD_LIBRARY_PATH=$OPENGAUSS_LIBRARY_PATH" \
    "$CONTAINER_NAME" "$OPENGAUSS_HOME/bin/gsql" "$@"
}

container_running() {
  [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" = "true" ]
}

container_exists() {
  docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1
}

validate_container_ownership() {
  if ! docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    return 0
  fi
  [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$CONTAINER_NAME")" = "opengauss-consumer-flow" ] ||
    fail "$CONTAINER_NAME is not owned by the openGauss consumer fixture"
}

reset_fixture() {
  opengauss_gsql -d "$DATABASE_NAME" -p 5432 <<SQL >/dev/null
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
  (1, 'OG-1001', 2, 19.90, TIMESTAMP '2026-09-01 08:00:00.000001'),
  (2, 'OG-1002', 4, 39.50, TIMESTAMP '2026-09-01 08:00:00.000002'),
  (3, 'OG-1003', 1, 99.00, TIMESTAMP '2026-09-01 08:00:00.000003'),
  (4, 'OG-1004', 8, 12.25, TIMESTAMP '2026-09-01 08:00:00.000004'),
  (5, 'OG-1005', 3, 50.75, TIMESTAMP '2026-09-01 08:00:00.000005');
SQL
  opengauss_gsql -At -d "$DATABASE_NAME" -p 5432 -c \
    "SELECT COUNT(*) FROM \"$SOURCE_TABLE\"" | grep -Fxq '5' ||
    fail "openGauss source fixture must contain exactly five rows"
  opengauss_gsql -At -d "$DATABASE_NAME" -p 5432 -c \
    "SELECT COUNT(*) FROM \"$TARGET_TABLE\"" | grep -Fxq '0' ||
    fail "openGauss target fixture must be empty after reset"
}

advance_fixture() {
  opengauss_gsql -d "$DATABASE_NAME" -p 5432 <<SQL >/dev/null
UPDATE "$SOURCE_TABLE"
SET quantity = 5, amount = 44.50, updated_at = TIMESTAMP '2026-09-02 09:30:00.000002'
WHERE id = 2;
MERGE INTO "$SOURCE_TABLE" AS target
USING (
  SELECT 6::BIGINT AS id, 'OG-1006'::VARCHAR(32) AS item_code,
         6::INTEGER AS quantity, 66.60::NUMERIC(12, 2) AS amount,
         TIMESTAMP '2026-09-02 09:30:00.000006' AS updated_at
) AS source
ON target.id = source.id
WHEN MATCHED THEN UPDATE SET
  item_code = source.item_code,
  quantity = source.quantity,
  amount = source.amount,
  updated_at = source.updated_at
WHEN NOT MATCHED THEN INSERT (id, item_code, quantity, amount, updated_at)
VALUES (source.id, source.item_code, source.quantity, source.amount, source.updated_at);
SQL
  opengauss_gsql -At -d "$DATABASE_NAME" -p 5432 -c \
    "SELECT COUNT(*) FROM \"$SOURCE_TABLE\" WHERE updated_at > TIMESTAMP '2026-09-01 08:00:00.000005'" |
    grep -Fxq '2' || fail "openGauss fixture advance must expose exactly two watermark rows"
}

validate_engine_descriptor_path() {
  local resolved_descriptor
  [ -n "${ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE:-}" ] ||
    fail "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE is required"
  case "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" in
    /*) ;;
    *) fail "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE must be absolute" ;;
  esac
  command -v python3 >/dev/null 2>&1 || fail "missing required command: python3"
  resolved_descriptor=$(python3 - "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" <<'PY'
import os
import sys

print(os.path.realpath(sys.argv[1]))
PY
  )
  case "$resolved_descriptor" in
    "$ROOT_DIR"|"$ROOT_DIR"/*)
      fail "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE must be outside the repository"
      ;;
  esac
}

write_engine_descriptor() {
  validate_engine_descriptor_path
  local host_mapping host_port temporary
  host_mapping=$(docker port "$CONTAINER_NAME" 5432/tcp)
  host_port=${host_mapping##*:}
  [[ "$host_port" =~ ^[0-9]+$ ]] || fail "cannot resolve openGauss host port"
  mkdir -p "$(dirname "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE")"
  temporary="$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE.tmp"
  umask 077
  python3 - "$temporary" "$host_port" "$DATABASE_NAME" "$DATABASE_USER" "$DATABASE_PASSWORD" <<'PY'
import json
import pathlib
import sys

path, port, database, user, password = sys.argv[1:]
payload = {
    "name": "Hosted Online openGauss",
    "engine_type": "opengauss",
    "engine_origin": "general",
    "connection_info": {
        "host": "127.0.0.1",
        "port": int(port),
        "database": database,
        "user": user,
        "password": password,
        "sslmode": "disable",
    },
    "description": "Disposable GitHub Hosted openGauss consumer-flow fixture",
}
pathlib.Path(path).write_text(json.dumps(payload, sort_keys=True) + "\n", encoding="utf-8")
PY
  chmod 600 "$temporary"
  mv "$temporary" "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
}

validate_container_ownership

case "$action" in
  start)
    validate_engine_descriptor_path
    if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
      fail "refusing to replace existing container: $CONTAINER_NAME"
    fi
    if docker image inspect "$OPENGAUSS_OFFICIAL_IMAGE" >/dev/null 2>&1; then
      fail "refusing to reuse existing image: $OPENGAUSS_OFFICIAL_IMAGE"
    fi
    opengauss_ensure_official_image x86_64
    export GS_PASSWORD=$DATABASE_PASSWORD
    docker run --detach \
      --name "$CONTAINER_NAME" \
      --label com.addp.online-fixture=opengauss-consumer-flow \
      --privileged=true \
      --publish 127.0.0.1::5432 \
      --env GS_PASSWORD \
      "$OPENGAUSS_OFFICIAL_IMAGE" >/dev/null
    unset GS_PASSWORD
    for _ in $(seq 1 120); do
      if container_running && opengauss_gsql -At -d postgres -p 5432 -c 'SELECT 1' 2>/dev/null | grep -Fxq '1'; then
        opengauss_gsql -d postgres -p 5432 -c "CREATE DATABASE $DATABASE_NAME DBCOMPATIBILITY 'PG'" >/dev/null
        reset_fixture
        write_engine_descriptor
        echo "Online openGauss consumer fixture is ready"
        exit 0
      fi
      container_running || fail "openGauss disposable container exited before readiness"
      sleep 5
    done
    fail "openGauss disposable container did not become ready within 600 seconds"
    ;;
  advance)
    container_running || fail "$CONTAINER_NAME is not running"
    advance_fixture
    echo "Online openGauss consumer fixture advanced by two watermark rows"
    ;;
  stop)
    if container_exists; then
      if container_running; then
        reset_fixture || true
      fi
      docker rm --force "$CONTAINER_NAME" >/dev/null
    fi
    container_exists && fail "$CONTAINER_NAME still exists after cleanup"
    echo "Online openGauss consumer fixture is stopped"
    ;;
  status)
    container_running || fail "$CONTAINER_NAME is not running"
    opengauss_gsql -At -d "$DATABASE_NAME" -p 5432 -c 'SELECT 1' | grep -Fxq '1' ||
      fail "openGauss fixture database is not ready"
    echo "Online openGauss consumer fixture is ready"
    ;;
esac
