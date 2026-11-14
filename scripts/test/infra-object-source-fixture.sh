#!/usr/bin/env bash
# A business PostgreSQL owned exclusively by the pinned object-storage T4.
set -euo pipefail
fail() { echo "Object T4 fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "${ONLINE_SUITE:-}" = infra-object-storage ] && [ "${POSTGRES_DB:-}" = addp_online ] || fail 'isolated Hosted object suite required'
container=addp-object-online-disposable
case "${1:-}" in
  stop)
    if docker container inspect "$container" >/dev/null 2>&1; then
      [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = infra-object-storage ] || fail 'container ownership mismatch'
      docker rm -fv "$container" >/dev/null
      ! docker container inspect "$container" >/dev/null 2>&1 || fail 'container remains'
    fi
    exit 0 ;;
  start) ;;
  *) fail 'usage: infra-object-source-fixture.sh start|stop' ;;
esac
! docker container inspect "$container" >/dev/null 2>&1 || fail 'refusing existing container'
[ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] || fail 'secret directory missing'
docker run -d --name "$container" --label com.addp.online-fixture=infra-object-storage \
  -p 127.0.0.1:55433:5432 -e POSTGRES_PASSWORD -e POSTGRES_DB=object_fixture addp-postgres-pgvector:latest >/dev/null
ready=0
for _ in $(seq 1 60); do
  if [ "$(PGPASSWORD="$POSTGRES_PASSWORD" docker exec -e PGPASSWORD "$container" psql -h 127.0.0.1 -U postgres -d object_fixture -Atqc 'SELECT current_database()' 2>/dev/null)" = object_fixture ]; then
    ready=1; break
  fi
  sleep 1
done
[ "$ready" = 1 ] || fail 'database readiness timed out'
export OBJECT_SOURCE_PASSWORD
OBJECT_SOURCE_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
docker exec -i "$container" psql -X -U postgres -d object_fixture -v ON_ERROR_STOP=1 -v reader_password="$OBJECT_SOURCE_PASSWORD" >/dev/null <<'SQL'
CREATE EXTENSION postgis;
CREATE ROLE object_operator LOGIN PASSWORD :'reader_password';
GRANT CONNECT ON DATABASE object_fixture TO object_operator;
GRANT USAGE, CREATE ON SCHEMA public TO object_operator;
SET ROLE object_operator;
-- An empty target allows a short relation lock to expose the real import
-- Worker's running lease before its replace operation. No fixture rows exist.
CREATE TABLE public.object_import_points (id integer, label text, shape geometry(Point,4326));
SQL
python3 - <<'PY'
import json, os
fd = os.open(os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE'], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'name': 'Object T4 PostgreSQL', 'engine_type': 'postgresql', 'engine_origin': 'general',
               'connection_info': {'host': '127.0.0.1', 'port': 55433, 'database': 'object_fixture',
                                   'user': 'object_operator', 'password': os.environ['OBJECT_SOURCE_PASSWORD'], 'sslmode': 'disable'}}, output)
PY
unset OBJECT_SOURCE_PASSWORD
