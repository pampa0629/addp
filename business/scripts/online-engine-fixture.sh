#!/usr/bin/env bash
# Lifecycle owner for the dedicated PostgreSQL Engine Fixture used by ADDP T4 Online gates.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)

fail() {
  echo "Online engine fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online engine fixture requires macOS"

action=${1:-}
case "$action" in
  start|stop|status) ;;
  *) fail "usage: bash business/scripts/online-engine-fixture.sh start|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_TEST_ENGINE_PORT
  ADDP_ONLINE_TEST_ENGINE_USER
  ADDP_ONLINE_TEST_ENGINE_PASSWORD
  ADDP_ONLINE_TEST_ENGINE_DATABASE
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done
[[ "$ADDP_ONLINE_TEST_ENGINE_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_TEST_ENGINE_PORT must be numeric"
[ "$ADDP_ONLINE_TEST_ENGINE_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_TEST_ENGINE_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_TEST_ENGINE_PORT must be between 1024 and 65535"

case "$(uname -m)" in
  arm64|aarch64) default_image=imresamu/postgis-arm64:15-3.4 ;;
  x86_64) default_image=postgis/postgis:15-3.4 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

docker_fixture() {
  env \
    POSTGRES_IMAGE="${ADDP_ONLINE_TEST_ENGINE_IMAGE:-$default_image}" \
    POSTGRES_PORT="$ADDP_ONLINE_TEST_ENGINE_PORT" \
    POSTGRES_USER="$ADDP_ONLINE_TEST_ENGINE_USER" \
    POSTGRES_PASSWORD="$ADDP_ONLINE_TEST_ENGINE_PASSWORD" \
    POSTGRES_DB="$ADDP_ONLINE_TEST_ENGINE_DATABASE" \
    docker "$@"
}

compose() {
  docker_fixture compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

container_running() {
  [ "$(docker_fixture inspect --format '{{.State.Running}}' business-postgres 2>/dev/null || true)" = "true" ]
}

seed_catalog_fixture() {
  docker_fixture exec business-postgres psql \
    -v ON_ERROR_STOP=1 \
    -U "$ADDP_ONLINE_TEST_ENGINE_USER" \
    -d "$ADDP_ONLINE_TEST_ENGINE_DATABASE" \
    -c 'CREATE TABLE IF NOT EXISTS public.addp_online_catalog_fixture (
      id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
      fixture_key text NOT NULL UNIQUE,
      fixture_value text NOT NULL,
      updated_at timestamptz NOT NULL DEFAULT now()
    );
    INSERT INTO public.addp_online_catalog_fixture (fixture_key, fixture_value)
    VALUES ('"'"'stable'"'"', '"'"'ADDP Online enterprise catalog fixture'"'"')
    ON CONFLICT (fixture_key) DO UPDATE
      SET fixture_value = EXCLUDED.fixture_value, updated_at = now();' >/dev/null
}

validate_container_ownership() {
  if ! docker_fixture inspect business-postgres >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_fixture inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-postgres)
  [ "$ownership" = "business/postgres" ] ||
    fail "business-postgres is not owned by the business/postgres Compose service"
}

validate_container_ownership

case "$action" in
  start)
    compose up -d postgres
    for _ in $(seq 1 60); do
      if container_running && docker_fixture exec business-postgres pg_isready \
        -U "$ADDP_ONLINE_TEST_ENGINE_USER" \
        -d "$ADDP_ONLINE_TEST_ENGINE_DATABASE" >/dev/null 2>&1; then
        seed_catalog_fixture
        echo "Online PostgreSQL Engine Fixture is ready on port $ADDP_ONLINE_TEST_ENGINE_PORT"
        exit 0
      fi
      sleep 1
    done
    fail "business-postgres did not become ready"
    ;;
  stop)
    compose rm -sf postgres
    if container_running; then
      fail "business-postgres is still running"
    fi
    echo "Online PostgreSQL Engine Fixture is stopped"
    ;;
  status)
    container_running || fail "business-postgres is not running"
    docker_fixture exec business-postgres pg_isready \
      -U "$ADDP_ONLINE_TEST_ENGINE_USER" \
      -d "$ADDP_ONLINE_TEST_ENGINE_DATABASE" >/dev/null 2>&1 ||
      fail "business-postgres is not ready"
    echo "Online PostgreSQL Engine Fixture is ready"
    ;;
esac
