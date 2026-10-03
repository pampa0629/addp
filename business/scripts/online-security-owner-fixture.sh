#!/usr/bin/env bash
# Physical MySQL and PostGIS fixtures for the disposable Security owner gate.
set -euo pipefail
umask 077
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
fail() { echo "Security owner fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail "Hosted Linux x86_64 is required"
[ "$#" -eq 1 ] || fail "usage: online-security-owner-fixture.sh start|stop"
docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable"
containers=(addp-security-online-postgres addp-security-online-mysql)
case "$1" in
  stop)
    # Check both owners before removing either container.
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
[ ! -e "$ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR" ] && [ ! -e "$ADDP_ONLINE_SECURITY_MYSQL_DESCRIPTOR" ] || fail "descriptor already exists"
export POSTGRES_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PWD
export SECURITY_POSTGRES_WRITER_PASSWORD SECURITY_MYSQL_READER_PASSWORD
POSTGRES_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MYSQL_ROOT_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
SECURITY_POSTGRES_WRITER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
SECURITY_MYSQL_READER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
MYSQL_PWD=$MYSQL_ROOT_PASSWORD
# The repository-owned Infra image includes PostGIS; never use the shared Infra database.
docker run -d --name "${containers[0]}" --label com.addp.online-fixture=security \
  --tmpfs /var/lib/postgresql/data -p 127.0.0.1:55433:5432 \
  -e POSTGRES_PASSWORD -e POSTGRES_DB=security_fixture addp-postgres-pgvector:latest >/dev/null
docker run -d --name "${containers[1]}" --label com.addp.online-fixture=security \
  --tmpfs /var/lib/mysql -p 127.0.0.1:53306:3306 -e MYSQL_ROOT_PASSWORD \
  -e MYSQL_DATABASE=security_fixture \
  mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b >/dev/null
ready=0
for _ in $(seq 1 90); do
  if docker exec "${containers[0]}" pg_isready -U postgres -d security_fixture >/dev/null 2>&1 &&
    docker exec -e MYSQL_PWD "${containers[1]}" mysql -h127.0.0.1 -uroot --batch -e 'SELECT 1' >/dev/null 2>&1; then ready=1; break; fi
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
CREATE ROLE security_writer LOGIN PASSWORD :'writer_password';
-- Provider write preparation issues CREATE SCHEMA IF NOT EXISTS, which checks database CREATE first.
GRANT CONNECT, CREATE ON DATABASE security_fixture TO security_writer;
GRANT USAGE, CREATE ON SCHEMA addp_online_security TO security_writer;
GRANT SELECT ON addp_online_security.spatial_algorithm_source TO security_writer;
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
python3 - <<'PY'
import json, os
for key, kind, port, user, password in (
    ('ADDP_ONLINE_SECURITY_POSTGRES_DESCRIPTOR', 'postgresql', 55433, 'security_writer', 'SECURITY_POSTGRES_WRITER_PASSWORD'),
    ('ADDP_ONLINE_SECURITY_MYSQL_DESCRIPTOR', 'mysql', 53306, 'security_reader', 'SECURITY_MYSQL_READER_PASSWORD'),
):
    connection = dict(host='127.0.0.1', port=port, database='security_fixture', user=user, password=os.environ[password])
    if kind == 'postgresql':
        connection['sslmode'] = 'disable'
    with os.fdopen(os.open(os.environ[key], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as output:
        json.dump(dict(name='Hosted Security ' + kind, engine_type=kind, engine_origin='general',
                       description='Disposable Security four-owner fixture', connection_info=connection), output)
PY
unset POSTGRES_PASSWORD MYSQL_ROOT_PASSWORD MYSQL_PWD SECURITY_POSTGRES_WRITER_PASSWORD SECURITY_MYSQL_READER_PASSWORD
echo "Disposable Security databases are ready"
