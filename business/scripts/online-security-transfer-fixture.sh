#!/usr/bin/env bash
# Lifecycle owner for the MongoDB -> PostgreSQL Security/Transfer T4 fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)

fail() {
  echo "Online Security Transfer fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online Security Transfer fixture requires macOS"

action=${1:-}
case "$action" in
  start|stop|status) ;;
  *) fail "usage: bash business/scripts/online-security-transfer-fixture.sh start|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_TEST_ENGINE_PORT
  ADDP_ONLINE_TEST_ENGINE_USER
  ADDP_ONLINE_TEST_ENGINE_PASSWORD
  ADDP_ONLINE_TEST_ENGINE_DATABASE
  ADDP_ONLINE_SECURITY_MONGODB_PORT
  ADDP_ONLINE_SECURITY_MONGODB_DATABASE
  ADDP_ONLINE_SECURITY_MONGODB_USER
  ADDP_ONLINE_SECURITY_MONGODB_PASSWORD
  ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER
  ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done

[[ "$ADDP_ONLINE_SECURITY_MONGODB_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_SECURITY_MONGODB_PORT must be numeric"
[ "$ADDP_ONLINE_SECURITY_MONGODB_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_SECURITY_MONGODB_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_SECURITY_MONGODB_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_SECURITY_MONGODB_DATABASE" =~ ^[A-Za-z][A-Za-z0-9_]{0,62}$ ]] ||
  fail "ADDP_ONLINE_SECURITY_MONGODB_DATABASE must be a safe MongoDB database name"
[[ "$ADDP_ONLINE_SECURITY_MONGODB_USER" =~ ^[A-Za-z][A-Za-z0-9_]{0,31}$ ]] ||
  fail "ADDP_ONLINE_SECURITY_MONGODB_USER must be a safe MongoDB account name"
for password_variable in ADDP_ONLINE_SECURITY_MONGODB_PASSWORD ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD; do
  [[ "${!password_variable}" =~ ^[A-Za-z0-9_.-]{16,128}$ ]] ||
    fail "$password_variable must contain 16-128 URL-safe characters"
done
[[ "$ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER" =~ ^[A-Za-z][A-Za-z0-9_]{0,31}$ ]] ||
  fail "ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER must be a safe MongoDB account name"

docker_mongodb() {
  env \
    MONGO_PORT="$ADDP_ONLINE_SECURITY_MONGODB_PORT" \
    MONGO_DB="$ADDP_ONLINE_SECURITY_MONGODB_DATABASE" \
    MONGO_ROOT_USER="$ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER" \
    MONGO_ROOT_PASSWORD="$ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD" \
    docker "$@"
}

compose_mongodb() {
  docker_mongodb compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

mongodb_running() {
  [ "$(docker_mongodb inspect --format '{{.State.Running}}' business-mongodb 2>/dev/null || true)" = "true" ]
}

root_mongosh() {
  docker_mongodb exec -i \
    -e ADDP_FIXTURE_DATABASE="$ADDP_ONLINE_SECURITY_MONGODB_DATABASE" \
    -e ADDP_FIXTURE_USER="$ADDP_ONLINE_SECURITY_MONGODB_USER" \
    -e ADDP_FIXTURE_PASSWORD="$ADDP_ONLINE_SECURITY_MONGODB_PASSWORD" \
    -e ADDP_FIXTURE_ROOT_USER="$ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER" \
    -e ADDP_FIXTURE_ROOT_PASSWORD="$ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD" \
    business-mongodb sh -lc \
    'mongosh --quiet --host 127.0.0.1 --authenticationDatabase admin --username "$ADDP_FIXTURE_ROOT_USER" --password "$ADDP_FIXTURE_ROOT_PASSWORD" "$@"' \
    fixture-mongosh "$@"
}

reader_mongosh() {
  docker_mongodb exec -i \
    -e ADDP_FIXTURE_DATABASE="$ADDP_ONLINE_SECURITY_MONGODB_DATABASE" \
    -e ADDP_FIXTURE_USER="$ADDP_ONLINE_SECURITY_MONGODB_USER" \
    -e ADDP_FIXTURE_PASSWORD="$ADDP_ONLINE_SECURITY_MONGODB_PASSWORD" \
    business-mongodb sh -lc \
    'mongosh --quiet --host 127.0.0.1 --authenticationDatabase "$ADDP_FIXTURE_DATABASE" --username "$ADDP_FIXTURE_USER" --password "$ADDP_FIXTURE_PASSWORD" "$@"' \
    fixture-mongosh "$@"
}

seed_mongodb_fixture() {
  root_mongosh --file /dev/stdin <<'JS' >/dev/null
const fixtureDatabase = process.env.ADDP_FIXTURE_DATABASE;
const fixtureUser = process.env.ADDP_FIXTURE_USER;
const fixturePassword = process.env.ADDP_FIXTURE_PASSWORD;
const fixture = db.getSiblingDB(fixtureDatabase);
fixture.security_transfer_fixture.deleteMany({});
fixture.security_transfer_fixture.insertMany([
  {_id: "person-1", displayName: "Alice", userInfo: {phone: "13812345678"}},
  {_id: "person-2", displayName: "Bob", userInfo: {phone: "13987654321"}},
  {_id: "person-3", displayName: "No phone"}
]);
if (fixture.getUser(fixtureUser)) {
  fixture.updateUser(fixtureUser, {pwd: fixturePassword, roles: [{role: "read", db: fixtureDatabase}]});
} else {
  fixture.createUser({user: fixtureUser, pwd: fixturePassword, roles: [{role: "read", db: fixtureDatabase}]});
}
JS
  reader_mongosh "$ADDP_ONLINE_SECURITY_MONGODB_DATABASE" --eval \
    'print(db.security_transfer_fixture.countDocuments({}))' | grep -qx '3' ||
    fail "Security Transfer MongoDB fixture must contain exactly three documents"
}

seed_postgresql_targets() {
  env \
    POSTGRES_PORT="$ADDP_ONLINE_TEST_ENGINE_PORT" \
    POSTGRES_USER="$ADDP_ONLINE_TEST_ENGINE_USER" \
    POSTGRES_PASSWORD="$ADDP_ONLINE_TEST_ENGINE_PASSWORD" \
    POSTGRES_DB="$ADDP_ONLINE_TEST_ENGINE_DATABASE" \
    docker exec business-postgres psql \
      -v ON_ERROR_STOP=1 \
      -U "$ADDP_ONLINE_TEST_ENGINE_USER" \
      -d "$ADDP_ONLINE_TEST_ENGINE_DATABASE" \
      -c 'CREATE SCHEMA IF NOT EXISTS addp_online_security;
          CREATE TABLE IF NOT EXISTS addp_online_security.transfer_excluded (
            _id text PRIMARY KEY,
            display_name text NOT NULL
          );
          CREATE TABLE IF NOT EXISTS addp_online_security.transfer_masked (
            _id text PRIMARY KEY,
            "userInfo__phone" text
          );
          CREATE TABLE IF NOT EXISTS addp_online_security.exemption_source (
            id bigint PRIMARY KEY,
            display_name text NOT NULL,
            phone text
          );
          CREATE TABLE IF NOT EXISTS addp_online_security.exemption_transfer (
            id bigint PRIMARY KEY,
            phone text
          );
          INSERT INTO addp_online_security.exemption_source (id, display_name, phone)
          VALUES
            (1, '"'"'Alice'"'"', '"'"'13812345678'"'"'),
            (2, '"'"'Bob'"'"', '"'"'13987654321'"'"'),
            (3, '"'"'No phone'"'"', NULL)
          ON CONFLICT (id) DO UPDATE
            SET display_name = EXCLUDED.display_name,
                phone = EXCLUDED.phone;
          TRUNCATE addp_online_security.transfer_excluded,
                   addp_online_security.transfer_masked,
                   addp_online_security.exemption_transfer;' >/dev/null
}

validate_container_ownership() {
  if ! docker_mongodb inspect business-mongodb >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_mongodb inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-mongodb)
  [ "$ownership" = "business/mongodb" ] ||
    fail "business-mongodb is not owned by the business/mongodb Compose service"
}

validate_container_ownership

case "$action" in
  start)
    bash "$SCRIPT_DIR/online-engine-fixture.sh" start
    compose_mongodb up -d mongodb
    for _ in $(seq 1 90); do
      if mongodb_running && root_mongosh --eval 'db.adminCommand({ping: 1})' >/dev/null 2>&1; then
        seed_mongodb_fixture
        seed_postgresql_targets
        echo "Online Security Transfer fixture is ready"
        exit 0
      fi
      sleep 1
    done
    fail "business-mongodb did not become ready"
    ;;
  stop)
    compose_mongodb rm -sf mongodb
    if mongodb_running; then
      fail "business-mongodb is still running"
    fi
    bash "$SCRIPT_DIR/online-engine-fixture.sh" stop
    echo "Online Security Transfer fixture is stopped"
    ;;
  status)
    bash "$SCRIPT_DIR/online-engine-fixture.sh" status
    mongodb_running || fail "business-mongodb is not running"
    reader_mongosh "$ADDP_ONLINE_SECURITY_MONGODB_DATABASE" --eval 'db.runCommand({ping: 1})' >/dev/null ||
      fail "business-mongodb read-only account is not ready"
    echo "Online Security Transfer fixture is ready"
    ;;
esac
