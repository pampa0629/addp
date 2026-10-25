#!/usr/bin/env bash
# Disposable PostgreSQL source for the Hosted metric lifecycle acceptance.
set -euo pipefail
fail() { echo "Metric PostgreSQL fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] || fail "Hosted Linux Online is required"
container=addp-metric-online-disposable
case "${1:-}" in
  snapshot-kml)
    [ "${ONLINE_SUITE:-}" = manager-internal-artifact-lineage ] &&
      [ "${ADDP_ONLINE_MANAGER_CASE:-}" = kml-export ] &&
      [ "${ADDP_ONLINE_TEST:-}" = 1 ] && [ "${POSTGRES_DB:-}" = addp_online ] || fail "KML snapshot requires its isolated case"
    [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = metric ] || fail "container ownership mismatch"
    docker exec -i "$container" psql -X -U postgres -d metric_fixture -Atq -v ON_ERROR_STOP=1 <<'SQL'
SELECT json_build_object(
  'wgs84', (SELECT md5(string_agg(row_to_json(t)::text, '' ORDER BY seq)) FROM public.kml_export_wgs84 t),
  'mercator', (SELECT md5(string_agg(row_to_json(t)::text, '' ORDER BY seq)) FROM public.kml_export_mercator t),
  'counts', ARRAY[(SELECT count(*) FROM public.kml_export_wgs84), (SELECT count(*) FROM public.kml_export_mercator)],
  'columns', (SELECT md5(string_agg(table_name||column_name||udt_name||is_nullable, '' ORDER BY table_name,ordinal_position))
    FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('kml_export_wgs84','kml_export_mercator')));
SQL
    exit 0
    ;;
  content-change|content-reset)
    [ "${ONLINE_SUITE:-}" = pg-source-authorization ] && [ "${ADDP_ONLINE_TEST:-}" = 1 ] &&
      [ "${POSTGRES_DB:-}" = addp_online ] || fail "PG authorization content changes require its isolated suite"
    [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = metric ] || fail "container ownership mismatch"
    change=0
    [ "$1" != content-change ] || change=1
    docker exec -i "$container" psql -U postgres -d metric_fixture -Atq -v ON_ERROR_STOP=1 -v change="$change" <<'SQL'
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '5s';
LOCK TABLE public.metric_people IN EXCLUSIVE MODE;
DO $$ BEGIN
  IF NOT COALESCE((SELECT array_agg(person_id ORDER BY person_id) FROM public.metric_people)
      IN (ARRAY['A','B']::text[], ARRAY['A','B2','C']::text[]), false) THEN
    RAISE EXCEPTION 'unexpected PG authorization source content';
  END IF;
END $$;
\if :change
UPDATE public.metric_people SET person_id = 'B2' WHERE person_id = 'B';
INSERT INTO public.metric_people VALUES ('C') ON CONFLICT DO NOTHING;
\else
DELETE FROM public.metric_people WHERE person_id = 'C';
UPDATE public.metric_people SET person_id = 'B' WHERE person_id = 'B2';
\endif
COMMIT;
\if :change
SELECT 'pg-content:changed' WHERE (SELECT array_agg(person_id ORDER BY person_id) FROM public.metric_people) = ARRAY['A','B2','C']::text[];
\else
SELECT 'pg-content:baseline' WHERE (SELECT array_agg(person_id ORDER BY person_id) FROM public.metric_people) = ARRAY['A','B']::text[];
\endif
SQL
    exit 0
    ;;
  stop)
    if docker container inspect "$container" >/dev/null 2>&1; then
      [ "$(docker inspect --format '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = metric ] || fail "container ownership mismatch"
      docker rm -fv "$container" >/dev/null
    fi
    ! docker container inspect "$container" >/dev/null 2>&1 || fail "container remains after cleanup"
    exit 0
    ;;
  start) ;;
  *) fail "usage: online-metric-postgres-fixture.sh start|stop|snapshot-kml|content-change|content-reset" ;;
esac
# The PG authorization suite also produces a genuinely new table. Keep all
# existing fixture tables read-only; CREATE only lets this disposable account
# own its new outputs. Other consumers of this shared fixture remain read-only.
allow_output_create=0
source_command=(postgres)
if [ "${ONLINE_SUITE:-}" = pg-source-authorization ]; then
  [ "${ADDP_ONLINE_TEST:-}" = 1 ] && [ "${POSTGRES_DB:-}" = addp_online ] ||
    fail "PG output creation requires its isolated suite"
  allow_output_create=1
  source_command+=(-c shared_preload_libraries=pg_stat_statements -c pg_stat_statements.track=all)
fi
! docker container inspect "$container" >/dev/null 2>&1 || fail "refusing an existing source container"
[ -d "${ADDP_ONLINE_SECRET_DIR:?}" ] || fail "Hosted secret directory is missing"
export POSTGRES_PASSWORD
POSTGRES_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
# Infra has just built this repository-owned image on the clean Hosted VM.
docker run -d --name "$container" --label com.addp.online-fixture=metric \
  -p 127.0.0.1:55433:5432 -e POSTGRES_PASSWORD -e POSTGRES_DB=metric_fixture \
  addp-postgres-pgvector:latest "${source_command[@]}" >/dev/null
ready=0
for _ in $(seq 1 60); do
  # The entrypoint first starts a socket-only bootstrap server before creating
  # POSTGRES_DB. Require the final TCP server and an actual database query.
  if [ "$(PGPASSWORD="$POSTGRES_PASSWORD" PGCONNECT_TIMEOUT=2 docker exec -e PGPASSWORD -e PGCONNECT_TIMEOUT \
      "$container" psql -h 127.0.0.1 -U postgres -d metric_fixture -Atqc 'SELECT current_database()' 2>/dev/null)" = metric_fixture ]; then
    ready=1
    break
  fi
  sleep 1
done
[ "$ready" = 1 ] || fail "source PostgreSQL did not become ready"
if [ "${ONLINE_SUITE:-}" = manager-internal-artifact-lineage ] && [ "${ADDP_ONLINE_MANAGER_CASE:-}" = kml-export ]; then
  [ "${ADDP_ONLINE_TEST:-}" = 1 ] && [ "${POSTGRES_DB:-}" = addp_online ] || fail "KML seed requires its isolated case"
  docker exec -i "$container" psql -X -U postgres -d metric_fixture -v ON_ERROR_STOP=1 \
    -v reader_password="$POSTGRES_PASSWORD" >/dev/null <<'SQL'
CREATE EXTENSION postgis;
CREATE TABLE public.kml_export_wgs84 (seq integer PRIMARY KEY, "标题" text, flag boolean NOT NULL, measure double precision NOT NULL, location_shape geometry(Geometry,4326));
INSERT INTO public.kml_export_wgs84
SELECT seq, CASE seq WHEN 1 THEN '' WHEN 2 THEN NULL ELSE '中文 <&> "quote"' END,
  seq % 2 = 0, 1.25, CASE WHEN wkt IS NULL THEN NULL ELSE ST_GeomFromText(wkt,4326) END
FROM (VALUES
 (0,'POINT (120 30)'), (1,'LINESTRING (120 30,121 31)'),
 (2,'POLYGON ((0 0,4 0,4 4,0 4,0 0),(1 1,1 2,2 2,2 1,1 1))'),
 (3,'MULTIPOINT ((1 2),(3 4))'), (4,'MULTILINESTRING ((1 2,3 4),(5 6,7 8))'),
 (5,'MULTIPOLYGON (((0 0,1 0,1 1,0 0)))'),
 (6,'GEOMETRYCOLLECTION (POINT (1 2),LINESTRING (3 4,5 6))'), (7,NULL)
) AS input(seq,wkt);
CREATE TABLE public.kml_export_mercator AS SELECT seq, "标题", flag, measure,
  ST_Transform(location_shape,3857)::geometry(Geometry,3857) AS location_shape FROM public.kml_export_wgs84;
CREATE ROLE metric_reader LOGIN PASSWORD :'reader_password';
GRANT CONNECT ON DATABASE metric_fixture TO metric_reader;
GRANT USAGE ON SCHEMA public TO metric_reader;
GRANT SELECT ON public.kml_export_wgs84, public.kml_export_mercator TO metric_reader;
SQL
else
docker exec -i "$container" psql -U postgres -d metric_fixture -v ON_ERROR_STOP=1 \
  -v reader_password="$POSTGRES_PASSWORD" -v allow_output_create="$allow_output_create" >/dev/null <<'SQL'
CREATE TABLE metric_people (person_id text PRIMARY KEY);
CREATE TABLE metric_events (event_id text PRIMARY KEY, event_date date NOT NULL);
CREATE TABLE metric_facts (row_id bigint PRIMARY KEY, person_id text NOT NULL, event_id text NOT NULL, leader boolean NOT NULL);
CREATE TABLE metric_areas (parcel_id bigint PRIMARY KEY, city varchar(100), area_m2 numeric(38,18) NOT NULL);
INSERT INTO metric_people VALUES ('A'), ('B');
INSERT INTO metric_events VALUES ('e1','2026-01-01'), ('e2','2026-02-01'), ('e3','2026-03-01'), ('old','2025-12-31');
INSERT INTO metric_facts VALUES (1,'A','e1',true), (2,'A','e1',true), (3,'A','e2',true), (4,'A','e3',false), (5,'A','old',true), (6,'B','e3',true);
INSERT INTO metric_areas VALUES (1,'长沙',9043526587.962176100000000001), (2,'长沙',2.500000000000000009), (3,'永州',11848801485.818872040000000007), (4,'',0.750000000000000003), (5,NULL,9.000000000000000000);
CREATE ROLE metric_reader LOGIN PASSWORD :'reader_password';
GRANT CONNECT ON DATABASE metric_fixture TO metric_reader;
GRANT USAGE ON SCHEMA public TO metric_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO metric_reader;
\if :allow_output_create
GRANT CREATE ON SCHEMA public TO metric_reader;
CREATE SCHEMA addp_observer AUTHORIZATION postgres;
CREATE EXTENSION pg_stat_statements WITH SCHEMA addp_observer;
\endif
SQL
fi
python3 - <<'PY'
import json, os
path = os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE']
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'name':'Hosted metric PostgreSQL', 'engine_type':'postgresql', 'engine_origin':'general',
               'description':'Disposable metric acceptance source', 'connection_info':{
                   'host':'127.0.0.1', 'port':55433, 'database':'metric_fixture', 'user':'metric_reader',
                   'password':os.environ['POSTGRES_PASSWORD'], 'sslmode':'disable'}}, output)
PY
echo "Disposable metric PostgreSQL source is ready"
