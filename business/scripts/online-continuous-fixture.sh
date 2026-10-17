#!/usr/bin/env bash
# Business-owned physical facts for the continuous execution event Hosted suite.
set -euo pipefail
fail() { echo "Continuous fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "${ADDP_ONLINE_HOST:-}" = 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'disposable Hosted Linux is required'
secret=${ADDP_ONLINE_SECRET_DIR:?}
[ -d "$secret" ] && [ "$(stat -c %a "$secret")" = 700 ] || fail 'private Hosted secret directory required'
[ "${POSTGRES_DB:-}" = addp_online ] || fail 'disposable application database required'
python3 - <<'CHECK'
import os,re
from pathlib import Path
secret=Path(os.environ['ADDP_ONLINE_SECRET_DIR']).resolve(strict=True)
runner=Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
if (secret.parent != runner or not re.fullmatch(r'addp-online-secret-[A-Za-z0-9_.-]+',secret.name)
    or secret.stat().st_uid != os.getuid() or secret.stat().st_mode & 0o777 != 0o700):
    raise SystemExit('private fixture ownership mismatch')
CHECK
owner=$(basename "$secret")
pg=addp-continuous-pg-online-disposable
kafka=addp-continuous-kafka-online-disposable
owned() {
  [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' "$1")" = continuous ] &&
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-run" }}' "$1")" = "$owner" ] || fail 'fixture ownership mismatch'
}
psql_owned() { owned "$pg"; docker exec -i "$pg" psql -U postgres -d continuous_fixture -v ON_ERROR_STOP=1 -At "$@"; }
action=${1:-}
case "$action" in
  stop)
    for container in "$kafka" "$pg"; do
      if docker container inspect "$container" >/dev/null 2>&1; then owned "$container"; docker rm -fv "$container" >/dev/null; fi
      ! docker container inspect "$container" >/dev/null 2>&1 || fail 'fixture container remains'
    done
    exit 0 ;;
  start)
    for container in "$pg" "$kafka"; do ! docker container inspect "$container" >/dev/null 2>&1 || fail 'refusing existing fixture'; done
    export POSTGRES_PASSWORD
    POSTGRES_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(32))')
    docker run -d --name "$pg" --network addp-network --label com.addp.online-fixture=continuous \
      --label "com.addp.online-run=$owner" -e POSTGRES_PASSWORD -e POSTGRES_DB=continuous_fixture \
      addp-postgres-pgvector:latest postgres -c wal_level=logical -c max_replication_slots=10 -c max_wal_senders=10 >/dev/null
    ready=0
    for _ in $(seq 1 60); do
      # The socket-only bootstrap server can accept probes before its shutdown.
      # Require the final TCP server and a query against the intended database.
      if [ "$(PGPASSWORD="$POSTGRES_PASSWORD" PGCONNECT_TIMEOUT=2 docker exec -e PGPASSWORD -e PGCONNECT_TIMEOUT \
          "$pg" psql -h 127.0.0.1 -U postgres -d continuous_fixture -Atqc 'SELECT current_database()' 2>/dev/null)" = continuous_fixture ]; then
        ready=1
        break
      fi
      sleep 1
    done
    [ "$ready" = 1 ] || fail 'source PostgreSQL did not become ready'
    psql_owned >/dev/null <<'SQL'
CREATE TABLE public.continuous_source (id bigint PRIMARY KEY, label text NOT NULL);
INSERT INTO public.continuous_source VALUES (1,'business-value-sentinel-initial'),(2,'business-value-sentinel-delete');
CREATE TABLE public.continuous_scan_sibling (id bigint PRIMARY KEY, label text NOT NULL);
INSERT INTO public.continuous_scan_sibling VALUES (1,'business-value-sentinel-sibling'),(2,'business-value-sentinel-sibling');
SQL
    docker run -d --name "$kafka" --label com.addp.online-fixture=continuous --label "com.addp.online-run=$owner" \
      -p 127.0.0.1:29094:29094 "${REDPANDA_IMAGE:?}" redpanda start --overprovisioned \
      --smp 1 --memory 512M --reserve-memory 0M --node-id 0 --check=false \
      --kafka-addr 0.0.0.0:29094 --advertise-kafka-addr 127.0.0.1:29094 >/dev/null
    ready=0
    for _ in $(seq 1 60); do if docker exec "$kafka" rpk cluster info -X brokers=127.0.0.1:29094 >/dev/null 2>&1; then ready=1; break; fi; sleep 1; done
    [ "$ready" = 1 ] || fail 'business Kafka did not become ready'
    docker exec "$kafka" rpk topic create continuous_events -p 2 -r 1 -X brokers=127.0.0.1:29094 >/dev/null
    export CONTINUOUS_PG_HOST
    CONTINUOUS_PG_HOST=$(docker inspect -f '{{(index .NetworkSettings.Networks "addp-network").IPAddress}}' "$pg")
    python3 - <<'PY'
import json,os
from pathlib import Path
root=Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
descriptors={
 'pg':{'name':'Hosted continuous PostgreSQL','description':'Disposable PostgreSQL CDC source and target','engine_type':'postgresql','engine_origin':'general','connection_info':{'host':os.environ['CONTINUOUS_PG_HOST'],'port':5432,'database':'continuous_fixture','user':'postgres','password':os.environ['POSTGRES_PASSWORD'],'sslmode':'disable'}},
 'kafka':{'name':'Hosted business Kafka','description':'Disposable business Kafka continuous source','engine_type':'kafka','engine_origin':'general','connection_info':{'bootstrap_servers':'127.0.0.1:29094'}},
}
for kind,value in descriptors.items():
 fd=os.open(root/f'continuous-{kind}.json',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as output: json.dump(value,output)
PY
    ;;
  produce-initial|produce-after-recovery|produce-invalid)
    owned "$kafka"
    case "$action" in
      produce-initial) records='{"id":1,"label":"business-value-sentinel-first"}
{"id":2,"label":"business-value-sentinel-second"}' ;;
      produce-after-recovery) records='{"id":1,"label":"business-value-sentinel-recovered"}
{"id":3,"label":"business-value-sentinel-third"}' ;;
      produce-invalid) records='{"label":"business-value-sentinel-invalid"}' ;;
    esac
    printf '%s\n' "$records" | docker exec -i "$kafka" rpk topic produce continuous_events -X brokers=127.0.0.1:29094 >/dev/null
    ;;
  mutate-cdc) psql_owned -c "UPDATE public.continuous_source SET label='business-value-sentinel-updated' WHERE id=1; DELETE FROM public.continuous_source WHERE id=2; INSERT INTO public.continuous_source VALUES (3,'business-value-sentinel-inserted');" >/dev/null ;;
  alter-cdc) psql_owned -c "ALTER TABLE public.continuous_source ADD COLUMN scan_extra text; UPDATE public.continuous_source SET scan_extra='business-value-sentinel-added' WHERE id=1;" >/dev/null ;;
  verify-cdc-altered)
    actual=$(psql_owned -c "SELECT id,label,scan_extra FROM public.continuous_cdc_target ORDER BY id" 2>/dev/null) || exit 1
    [ "$actual" = '1|business-value-sentinel-updated|business-value-sentinel-added
3|business-value-sentinel-inserted|' ] || exit 1
    ;;
  verify-kafka-initial|verify-kafka-recovered|verify-cdc-initial|verify-cdc-mutated)
    case "$action" in
      verify-kafka-initial) expected='1|business-value-sentinel-first
2|business-value-sentinel-second'; table=continuous_kafka_target ;;
      verify-kafka-recovered) expected='1|business-value-sentinel-recovered
2|business-value-sentinel-second
3|business-value-sentinel-third'; table=continuous_kafka_target ;;
      verify-cdc-initial) expected='1|business-value-sentinel-initial
2|business-value-sentinel-delete'; table=continuous_cdc_target ;;
      verify-cdc-mutated) expected='1|business-value-sentinel-updated
3|business-value-sentinel-inserted'; table=continuous_cdc_target ;;
    esac
    actual=$(psql_owned -c "SELECT id,label FROM public.$table ORDER BY id" 2>/dev/null) || exit 1
    [ "$actual" = "$expected" ] || exit 1
    ;;
  verify-scheduled)
    actual=$(psql_owned -c "SELECT id,label FROM public.continuous_scheduled_target ORDER BY id" 2>/dev/null) || exit 1
    [ "$actual" = '1|business-value-sentinel-sibling
2|business-value-sentinel-sibling' ] || exit 1
    ;;
  verify-clean)
    remaining=$(psql_owned -c "SELECT (SELECT count(*) FROM pg_replication_slots)+(SELECT count(*) FROM pg_publication WHERE pubname <> '');")
    [ "$remaining" = 0 ] || fail 'CDC slots or publications remain'
    ;;
  *) fail 'unsupported fixture action' ;;
esac
