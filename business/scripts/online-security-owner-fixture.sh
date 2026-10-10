#!/usr/bin/env bash
# Physical MySQL, PostGIS and MongoDB fixtures for the disposable Security owner gate.
set -euo pipefail
umask 077
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
fail() { echo "Security owner fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail "Hosted Linux x86_64 is required"
[ "$#" -eq 1 ] || fail "usage: online-security-owner-fixture.sh start|stop"
docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable"
containers=(addp-security-online-postgres addp-security-online-mysql addp-security-online-mongodb)
case "$1" in
  stop)
    # Check all owners before removing any container.
    for container in "${containers[@]}"; do
      if docker container inspect "$container" >/dev/null 2>&1; then
        [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = security ] || fail "container ownership mismatch"
      fi
    done
    for container in "${containers[@]}"; do
      if docker container inspect "$container" >/dev/null 2>&1; then
        docker rm -fv "$container" >/dev/null
      fi
      ! docker container inspect "$container" >/dev/null 2>&1 || fail "container remains after cleanup"
    done
    remaining=$(docker ps -aq --filter label=com.addp.online-fixture=security) || fail "cannot verify fixture cleanup"
    [ -z "$remaining" ] || fail "fixture containers remain after cleanup"
    exit 0 ;;
  start) ;;
  *) fail "usage: online-security-owner-fixture.sh start|stop" ;;
esac
for container in "${containers[@]}"; do
  ! docker container inspect "$container" >/dev/null 2>&1 || fail "refusing an existing fixture container"
done
remaining=$(docker ps -aq --filter label=com.addp.online-fixture=security) || fail "cannot inspect fixture containers"
[ -z "$remaining" ] || fail "refusing existing fixture resources"
[ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] || fail "Hosted secret directory is missing"
export ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR="$ADDP_ONLINE_SECRET_DIR/security-postgres.json"
export ADDP_ONLINE_SECURITY_MYSQL_DESCRIPTOR="$ADDP_ONLINE_SECRET_DIR/security-mysql.json"
export ADDP_ONLINE_SECURITY_MONGODB_DESCRIPTOR="$ADDP_ONLINE_SECRET_DIR/security-mongodb.json"
[ ! -e "$ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR" ] && [ ! -e "$ADDP_ONLINE_SECURITY_MYSQL_DESCRIPTOR" ] &&
  [ ! -e "$ADDP_ONLINE_SECURITY_MONGODB_DESCRIPTOR" ] || fail "descriptor already exists"
export POSTGRES_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PWD
export SECURITY_POSTGRES_WRITER_PASSWORD SECURITY_MYSQL_READER_PASSWORD
export MONGO_INITDB_ROOT_USERNAME=security_root MONGO_INITDB_ROOT_PASSWORD SECURITY_MONGODB_READER_PASSWORD
POSTGRES_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MYSQL_ROOT_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
SECURITY_POSTGRES_WRITER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
SECURITY_MYSQL_READER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MONGO_INITDB_ROOT_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
SECURITY_MONGODB_READER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MYSQL_PWD=$MYSQL_ROOT_PASSWORD
# The repository-owned Infra image includes PostGIS; never use the shared Infra database.
docker run -d --name "${containers[0]}" --label com.addp.online-fixture=security \
  --tmpfs /var/lib/postgresql/data -p 127.0.0.1:55433:5432 \
  -e POSTGRES_PASSWORD -e POSTGRES_DB=security_fixture addp-postgres-pgvector:latest >/dev/null
docker run -d --name "${containers[1]}" --label com.addp.online-fixture=security \
  --tmpfs /var/lib/mysql -p 127.0.0.1:53306:3306 -e MYSQL_ROOT_PASSWORD \
  -e MYSQL_DATABASE=security_fixture \
  mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b >/dev/null
docker run -d --name "${containers[2]}" --label com.addp.online-fixture=security \
  --tmpfs /data/db --tmpfs /data/configdb -p 127.0.0.1:57017:27017 \
  -e MONGO_INITDB_ROOT_USERNAME -e MONGO_INITDB_ROOT_PASSWORD mongo:7.0 >/dev/null
ready=0
for _ in $(seq 1 90); do
  if docker exec "${containers[0]}" pg_isready -U postgres -d security_fixture >/dev/null 2>&1 &&
    docker exec -e MYSQL_PWD "${containers[1]}" mysql -h127.0.0.1 -uroot --batch -e 'SELECT 1' >/dev/null 2>&1 &&
    docker exec "${containers[2]}" sh -lc 'mongosh --quiet --host 127.0.0.1 --authenticationDatabase admin --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --eval "db.adminCommand({ping: 1})"' >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || fail "business databases did not become ready"
# Seed errors stay in the disposable secret directory, never in evidence uploads.
if ! docker exec -i "${containers[0]}" psql -U postgres -d security_fixture -v ON_ERROR_STOP=1 \
  -v writer_password="$SECURITY_POSTGRES_WRITER_PASSWORD" >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/security-postgres-seed-error.log" <<'SQL'
CREATE SCHEMA addp_online_security;
CREATE EXTENSION postgis;
CREATE TABLE addp_online_security.spatial_algorithm_source (
  id bigint PRIMARY KEY, value_a text, value_b text, value_c text,
  location_point geometry(Point, 4326) NOT NULL
);
INSERT INTO addp_online_security.spatial_algorithm_source
  SELECT id, value, value, value, ST_SetSRID(ST_MakePoint(100 + id, 20 + id), 4326)
  FROM (VALUES (1, '13812345678'), (2, '张三abc'), (3, 'abc'), (4, NULL::text), (5, '')) AS fixture(id, value);
CREATE TABLE addp_online_security.spatial_algorithm_transfer (id bigint PRIMARY KEY);
CREATE TABLE addp_online_security.mysql_email_transfer (id bigint PRIMARY KEY);
CREATE TABLE addp_online_security.exemption_source (id bigint PRIMARY KEY, phone text);
INSERT INTO addp_online_security.exemption_source VALUES
  (1, '13812345678'), (2, '13987654321'), (3, NULL);
CREATE ROLE security_writer LOGIN PASSWORD :'writer_password';
-- Provider write preparation issues CREATE SCHEMA IF NOT EXISTS, which checks database CREATE first.
GRANT CONNECT, CREATE ON DATABASE security_fixture TO security_writer;
GRANT USAGE, CREATE ON SCHEMA addp_online_security TO security_writer;
GRANT SELECT ON addp_online_security.spatial_algorithm_source TO security_writer;
GRANT SELECT ON addp_online_security.exemption_source TO security_writer;
ALTER TABLE addp_online_security.spatial_algorithm_transfer OWNER TO security_writer;
ALTER TABLE addp_online_security.mysql_email_transfer OWNER TO security_writer;
SQL
then fail "PostgreSQL seed failed"; fi
if ! MYSQL_CONTAINER="${containers[1]}" MYSQL_USER=root MYSQL_DATABASE=security_fixture \
  bash "$ROOT_DIR/business/mysql/test-data.sh" >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/security-mysql-seed-error.log"; then fail "MySQL seed failed"; fi
if ! docker exec -i -e MYSQL_PWD "${containers[1]}" mysql -h127.0.0.1 -uroot security_fixture \
  >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/security-mysql-reader-error.log" <<SQL
CREATE USER 'security_reader'@'%' IDENTIFIED BY '$SECURITY_MYSQL_READER_PASSWORD';
GRANT SELECT ON security_fixture.* TO 'security_reader'@'%';
SQL
then fail "MySQL reader creation failed"; fi
if ! docker exec -i -e SECURITY_MONGODB_READER_PASSWORD "${containers[2]}" sh -lc \
  'mongosh --quiet --host 127.0.0.1 --authenticationDatabase admin --username "$MONGO_INITDB_ROOT_USERNAME" --password "$MONGO_INITDB_ROOT_PASSWORD" --file /dev/stdin' \
  >/dev/null 2>"$ADDP_ONLINE_SECRET_DIR/security-mongodb-seed-error.log" <<'JS'
const fixture = db.getSiblingDB("Outdoor");
const values = ["13812345678", "张三abc", "abc", null, ""];
fixture.Persons.insertMany(values.map((phone, index) => ({
  _id: String(index + 1), displayName: "person-" + (index + 1),
  userInfo: {phone, nickName: "nickname-" + (index + 1)}
})).concat([
  {_id: "6", displayName: "person-6", userInfo: {nickName: "nickname-6"}},
  {_id: "7", displayName: "person-7"}
]));
fixture.createUser({user: "security_reader", pwd: process.env.SECURITY_MONGODB_READER_PASSWORD,
  roles: [{role: "read", db: "Outdoor"}]});
if (fixture.Persons.countDocuments({}) !== 7) throw new Error("MongoDB fixture row count mismatch");
JS
then fail "MongoDB seed failed"; fi
python3 - <<'PY'
import json, os
for key, kind, port, user, password in (
    ('ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR', 'postgresql', 55433, 'security_writer', 'SECURITY_POSTGRES_WRITER_PASSWORD'),
    ('ADDP_ONLINE_SECURITY_MYSQL_DESCRIPTOR', 'mysql', 53306, 'security_reader', 'SECURITY_MYSQL_READER_PASSWORD'),
    ('ADDP_ONLINE_SECURITY_MONGODB_DESCRIPTOR', 'mongodb', 57017, 'security_reader', 'SECURITY_MONGODB_READER_PASSWORD'),
):
    connection = dict(host='127.0.0.1', port=port, database='security_fixture', user=user, password=os.environ[password])
    if kind == 'postgresql':
        connection['sslmode'] = 'disable'
    if kind == 'mongodb':
        connection.update(database='Outdoor', auth_source='Outdoor')
    with os.fdopen(os.open(os.environ[key], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as output:
        json.dump(dict(name='Hosted Security ' + kind, engine_type=kind, engine_origin='general',
                       description='Disposable Security four-owner fixture', connection_info=connection), output)
PY
unset POSTGRES_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PWD SECURITY_POSTGRES_WRITER_PASSWORD SECURITY_MYSQL_READER_PASSWORD
unset MONGO_INITDB_ROOT_USERNAME MONGO_INITDB_ROOT_PASSWORD SECURITY_MONGODB_READER_PASSWORD
echo "Disposable Security databases are ready"
