#!/usr/bin/env bash
# Business owns disposable Hadoop/Spark services and samples, never System registration.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SUITE=hdfs-spark-consumer-flow
LABEL=com.addp.online-fixture
CONTAINERS=(addp-hdfs-online-namenode addp-hdfs-online-datanode addp-hdfs-online-master addp-hdfs-online-worker addp-hdfs-online-postgres addp-hdfs-online-minio addp-hdfs-online-mysql addp-hdfs-online-doris)
fail() { echo "Online HDFS fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOST:-}" = 1 ] && [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] &&
  [ "${ADDP_ONLINE_OWNER_MANAGED:-0}" != 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'requires GitHub Hosted Linux x86_64'
[ "$#" -eq 1 ] || fail 'usage: start|stop'
stop() {
  local container remaining volume volumes='' status=0
  for container in "${CONTAINERS[@]}"; do
    if docker container inspect "$container" >/dev/null 2>&1; then
      if [ "$(docker container inspect --format "{{index .Config.Labels \"$LABEL\"}}" "$container")" != "$SUITE" ]; then
        echo 'refusing to delete a foreign container' >&2
        status=1
        continue
      fi
      volumes="$volumes $(docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{println .Name}}{{end}}{{end}}' "$container")" || status=1
      # Only the public disposable fixture enters diagnostics, before deletion.
      # Archive only State and public service logs; PostgreSQL credentials stay in owner-only files.
      if [ -n "${ADDP_ONLINE_ARTIFACT_DIR:-}" ]; then
        docker container inspect --format '{{json .State}}' "$container" > "$ADDP_ONLINE_ARTIFACT_DIR/$container-state.json" || status=1
        if [[ "$container" != *-postgres && "$container" != *-minio && "$container" != *-mysql && "$container" != *-doris ]]; then
          docker logs --tail 80 "$container" > "$ADDP_ONLINE_ARTIFACT_DIR/$container.log" 2>&1 || status=1
        fi
        if [[ "$container" = *-master || "$container" = *-worker ]]; then
          docker exec "$container" bash -c 'find /opt/spark/logs /opt/spark/work -name "*.out" -o -name stderr | while read -r file; do echo "$file"; tail -80 "$file"; done' > "$ADDP_ONLINE_ARTIFACT_DIR/$container-spark.log" 2>&1 || true
        fi
      fi
      docker rm -fv "$container" >/dev/null || status=1
    fi
  done
  for volume in $volumes; do
    if docker volume inspect "$volume" >/dev/null 2>&1; then
      echo 'fixture data volume remains' >&2
      status=1
    fi
  done
  remaining=$(docker ps -aq --filter "label=$LABEL=$SUITE") || status=1
  if [ -n "$remaining" ]; then echo 'fixture containers remain' >&2; status=1; fi
  return "$status"
}
case "$1" in stop) stop; exit 0;; start) ;; *) fail 'usage: start|stop';; esac
for container in "${CONTAINERS[@]}"; do
  if docker container inspect "$container" >/dev/null 2>&1; then fail 'refusing to reuse an existing container'; fi
done
python3 - <<'PY'
import os, stat
from pathlib import Path
root = Path(os.environ['ADDP_ONLINE_SECRET_DIR']).resolve(strict=True)
if stat.S_IMODE(root.stat().st_mode) != 0o700:
    raise SystemExit('secret directory must have mode 0700')
for name in ('hdfs-engine.json', 'spark-engine.json', 'postgres-engine.json', 'minio-engine.json', 'mysql-engine.json', 'doris-engine.json', 'mysql.env', 'minio.env', 'postgres.env', 'postgres-seed.sql', 'postgres-before.json'):
    output = root / name
    if output.exists() or output.is_symlink():
        raise SystemExit('descriptors must be new secret files')
PY
# Consume the same fixed official images as Business. No root or Business .env.
IMAGES=$(docker compose --env-file "$ROOT_DIR/business/.env.example" -f "$ROOT_DIR/business/docker-compose.yml" config --format json)
HADOOP_IMAGE=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["hdfs-namenode"]["image"])' <<< "$IMAGES")
SPARK_IMAGE=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["spark-master"]["image"])' <<< "$IMAGES")
# Reuse the T2's fixed AMD64 PostGIS build; registration still belongs to System.
POSTGRES_IMAGE=$(python3 - "$ROOT_DIR/scripts/test/docker-compose.hdfs-t2.yml" <<'IMAGE_PY'
from pathlib import Path
import re, sys
text = Path(sys.argv[1]).read_text().split('  postgres-amd64:', 1)[1].split('  hdfs-namenode:', 1)[0]
match = re.search(r'(?m)^    image: (postgis/postgis:15-3\.4@sha256:[a-f0-9]{64})$', text)
if not match:
    raise SystemExit('PostGIS requires a fixed T2 image')
print(match[1])
IMAGE_PY
)
[[ "$HADOOP_IMAGE" =~ ^ghcr.io/apache/hadoop:3\.5\.0@sha256:[a-f0-9]{64}$ ]] || fail 'Hadoop must use its fixed official digest'
[[ "$SPARK_IMAGE" =~ ^apache/spark:3\.5\.0-scala2\.12-java11-python3-ubuntu@sha256:[a-f0-9]{64}$ ]] || fail 'Spark must use its fixed official digest'
# Use the exact MySQL/Doris artifacts already certified by the real Worker T2.
table_image() {
  python3 - "$ROOT_DIR/scripts/test/common-spark-table-gate.sh" "$1" <<'PY'
import re, sys
from pathlib import Path
match = re.search(r"(?m)^" + sys.argv[2] + r"_IMAGE='([^']+@sha256:[a-f0-9]{64})'$", Path(sys.argv[1]).read_text())
if not match:
    raise SystemExit('Spark table T2 requires a fixed image digest')
print(match[1])
PY
}
MYSQL_IMAGE=$(table_image MYSQL)
DORIS_IMAGE=$(table_image DORIS)
# Reuse the freshly built Infra binary image, with independent Business data/credentials.
MINIO_IMAGE=$(docker container inspect --format '{{.Image}}' addp-minio)
[[ "$MINIO_IMAGE" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'MinIO must use the current immutable Infra build'
# Check all fixed ports before claiming any resource.
python3 - <<'PY'
import socket
for port in (8020, 9870, 9864, 9866, 9867, 7077, 10000, 18080, 18081, 15435, 15436, 15437, 15438, 8030, 9030, 8040, 9050, 9060, 8060):
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', port))
PY
created=0
finish_start() {
  local status=$?
  trap - EXIT INT TERM
  if [ "$status" -ne 0 ] && [ "$created" -eq 1 ]; then
    stop || status=1
    rm -f "$ADDP_ONLINE_SECRET_DIR"/{hdfs-engine.json,spark-engine.json,postgres-engine.json,minio-engine.json,mysql-engine.json,doris-engine.json,mysql.env,minio.env,postgres.env,postgres-seed.sql,postgres-before.json}
  fi
  exit "$status"
}
trap finish_start EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Host networking is confined to this Linux disposable profile: every consumer
# (native Driver, JVM Executor and WebHDFS) reaches the same loopback endpoints.
created=1
# Business owns PostgreSQL samples and private database credentials.
python3 - <<'POSTGRES_PY'
import json, os, secrets
from pathlib import Path
root = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
admin_password, writer_password = secrets.token_hex(24), secrets.token_hex(24)
minio_access, minio_secret = 'online_' + secrets.token_hex(12), secrets.token_hex(24)
mysql_password = secrets.token_hex(24)
columns = ', '.join(f'{name}_{suffix} bigint NOT NULL CHECK ({name}_{suffix} >= 0)'
                    for name in ('csv', 'json', 'parquet') for suffix in ('rows', 'amount_sum'))
values = {
 'mysql.env': f'MYSQL_ROOT_PASSWORD={mysql_password}\nMYSQL_DATABASE=spark_results\n',
 'mysql-engine.json': json.dumps({'name': 'Hosted MySQL', 'engine_type': 'mysql', 'engine_origin': 'general',
    'description': 'Disposable Spark MySQL publication acceptance',
    'connection_info': {'host': '127.0.0.1', 'port': 15438, 'database': 'spark_results', 'user': 'root', 'password': mysql_password}}),
 'doris-engine.json': json.dumps({'name': 'Hosted Doris', 'engine_type': 'doris', 'engine_origin': 'general',
    'description': 'Disposable Spark Doris publication acceptance',
    'connection_info': {'host': '127.0.0.1', 'port': 9030, 'http_port': 8030, 'database': 'spark_results', 'user': 'root', 'password': ''}}),
 'minio.env': f'MINIO_ROOT_USER={minio_access}\nMINIO_ROOT_PASSWORD={minio_secret}\n',
 'minio-engine.json': json.dumps({'name': 'Hosted MinIO', 'engine_type': 'minio',
    'engine_origin': 'general', 'description': 'Disposable Spark Parquet results',
    'connection_info': {'endpoint': '127.0.0.1:15436', 'access_key': minio_access,
                        'secret_key': minio_secret, 'use_ssl': False, 'bucket': 'result'}}),
 'postgres.env': f'POSTGRES_DB=spark_results\nPOSTGRES_USER=fixture_admin\nPOSTGRES_PASSWORD={admin_password}\n',
 'postgres-seed.sql': f"""CREATE ROLE spark_writer LOGIN PASSWORD '{writer_password}';
CREATE ROLE result_reader NOLOGIN;
CREATE SCHEMA results AUTHORIZATION spark_writer;
CREATE TABLE results.hdfs_totals(region text PRIMARY KEY, {columns});
CREATE INDEX hdfs_totals_region_index ON results.hdfs_totals(region);
COMMENT ON TABLE results.hdfs_totals IS 'preserve-hdfs-results';
GRANT SELECT,INSERT,DELETE,UPDATE ON results.hdfs_totals TO spark_writer;
GRANT SELECT ON results.hdfs_totals TO result_reader;
INSERT INTO results.hdfs_totals VALUES ('old', 0,0,0,0,0,0);
""",
 'postgres-engine.json': json.dumps({'name': 'Hosted PostgreSQL', 'engine_type': 'postgresql',
    'engine_origin': 'general', 'description': 'Disposable Spark persisted results',
    'connection_info': {'host': '127.0.0.1', 'port': 15435, 'database': 'spark_results',
                        'user': 'spark_writer', 'password': writer_password, 'sslmode': 'disable'}}),
}
for name, content in values.items():
    with os.fdopen(os.open(root / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
        stream.write(content)
POSTGRES_PY
docker run -d --name addp-hdfs-online-mysql --label "$LABEL=$SUITE" --network host --memory 1g \
  --env-file "$ADDP_ONLINE_SECRET_DIR/mysql.env" --tmpfs /var/lib/mysql \
  "$MYSQL_IMAGE" --port=15438 --bind-address=127.0.0.1 --mysqlx=0 >/dev/null
docker run -d --name addp-hdfs-online-doris --label "$LABEL=$SUITE" --network host --memory 3g \
  --add-host host.docker.internal:127.0.0.1 --add-host doris:127.0.0.1 --hostname doris -e BUILD_TYPE=production \
  --mount "type=bind,source=$ROOT_DIR/business/doris/init.sh,target=/addp/doris/init.sh,readonly" \
  --entrypoint bash "$DORIS_IMAGE" -c '
    set -euo pipefail
    sed -i "s/-Xmx8192m/-Xmx1024m/g; s/-Xms8192m/-Xms1024m/g" /opt/apache-doris/fe/conf/fe.conf
    echo "mem_limit = 1024m" >> /opt/apache-doris/be/conf/be.conf
    exec bash /addp/doris/init.sh' >/dev/null
table_readiness_diagnostics() {
  local diagnostics
  diagnostics=$(mktemp -d "$ADDP_ONLINE_SECRET_DIR/table-readiness.XXXXXX")
  docker exec addp-hdfs-online-mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -h127.0.0.1 -P15438 -uroot --connect-timeout=2 -e "SELECT 1"' > "$diagnostics/mysql-probe.txt" 2>&1 || true
  docker exec addp-hdfs-online-doris mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=2 --batch -e 'SHOW FRONTENDS; SHOW BACKENDS;' > "$diagnostics/doris-probe.txt" 2>&1 || true
  docker logs addp-hdfs-online-doris 2>&1 | head -n 80 > "$diagnostics/doris-startup-head.txt" || true
  docker logs --tail 100 addp-hdfs-online-doris > "$diagnostics/doris-startup.txt" 2>&1 || true
  docker exec addp-hdfs-online-doris bash -c '
    for file in /opt/apache-doris/fe/log/fe.out /opt/apache-doris/fe/log/fe.log /opt/apache-doris/fe/log/fe.warn.log; do
      if [ -f "$file" ]; then printf "FE log: %s\n" "$file"; tail -n 80 "$file"; fi
    done
    printf "FE hostname resolution\n"
    hostname
    getent hosts "$(hostname)" host.docker.internal
    ps -eo pid,comm' > "$diagnostics/doris-fe.txt" 2>&1 || true
  python3 - "$diagnostics" "$mysql_ready" "$doris_ready" <<'PY'
import json, os, shutil, sys
from pathlib import Path
root = Path(sys.argv[1])
secret = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
password = next(line.split('=', 1)[1] for line in (secret / 'mysql.env').read_text().splitlines() if line.startswith('MYSQL_ROOT_PASSWORD='))
try:
    report = {'mysql_ready': sys.argv[2] == '1', 'doris_ready': sys.argv[3] == '1'}
    for name in ('mysql-probe', 'doris-probe', 'doris-startup-head', 'doris-startup', 'doris-fe'):
        report[name] = (root / (name + '.txt')).read_text(errors='replace').replace(password, '<redacted>')[-12000:]
    text = json.dumps(report, ensure_ascii=False)
    if os.environ.get('ADDP_ONLINE_ARTIFACT_DIR'):
        Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'], 'spark-table-readiness.json').write_text(text)
    print('Spark table readiness diagnostics: ' + text)
finally:
    shutil.rmtree(root)
PY
}
mysql_ready=0 doris_ready=0
for attempt in $(seq 1 120); do
  mysql_ready=0 doris_ready=0
  if docker exec addp-hdfs-online-mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -h127.0.0.1 -P15438 -uroot --connect-timeout=2 -e "SELECT 1"' >/dev/null 2>&1; then mysql_ready=1; fi
  if docker exec addp-hdfs-online-doris bash /addp/doris/init.sh --health >/dev/null 2>&1; then doris_ready=1; fi
  if [ "$mysql_ready$doris_ready" = 11 ]; then break; fi
  if [ "$attempt" -eq 120 ]; then
    table_readiness_diagnostics
    fail "MySQL/Doris readiness timed out (mysql=$mysql_ready doris=$doris_ready)"
  fi
  sleep 2
done
docker exec addp-hdfs-online-doris mysql -h127.0.0.1 -P9030 -uroot -e 'CREATE DATABASE spark_results' >/dev/null
docker run -d --name addp-hdfs-online-minio --label "$LABEL=$SUITE" --network host --memory 512m \
  --env-file "$ADDP_ONLINE_SECRET_DIR/minio.env" --tmpfs /data \
  "$MINIO_IMAGE" server /data --address 127.0.0.1:15436 --console-address 127.0.0.1:15437 >/dev/null
for attempt in $(seq 1 60); do
  if docker exec addp-hdfs-online-minio curl -fsS http://127.0.0.1:15436/minio/health/ready >/dev/null; then break; fi
  [ "$attempt" -lt 60 ] || fail 'MinIO readiness timed out'
  sleep 1
done
docker exec addp-hdfs-online-minio sh -c 'mc alias set owned http://127.0.0.1:15436 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null && mc mb owned/result >/dev/null'
docker run -d --name addp-hdfs-online-postgres --label "$LABEL=$SUITE" --network host --memory 512m \
  --env-file "$ADDP_ONLINE_SECRET_DIR/postgres.env" --tmpfs /var/lib/postgresql/data \
  "$POSTGRES_IMAGE" postgres -c listen_addresses=127.0.0.1 -p 15435 >/dev/null
for attempt in $(seq 1 60); do
  if docker exec addp-hdfs-online-postgres pg_isready -p 15435 -U fixture_admin -d spark_results >/dev/null; then break; fi
  [ "$attempt" -lt 60 ] || fail 'PostgreSQL readiness timed out'
  sleep 1
done
docker exec -i addp-hdfs-online-postgres psql -X -v ON_ERROR_STOP=1 -p 15435 -U fixture_admin -d spark_results < "$ADDP_ONLINE_SECRET_DIR/postgres-seed.sql" >/dev/null
# Public structural receipt: credentials and SQL seed never enter artifacts.
docker exec addp-hdfs-online-postgres psql -XAt -p 15435 -U fixture_admin -d spark_results -c \
  "SELECT json_build_object('oid',oid::text,'acl',relacl::text,'comment',obj_description(oid)) FROM pg_class WHERE oid='results.hdfs_totals'::regclass" > "$ADDP_ONLINE_SECRET_DIR/postgres-before.json"
chmod 600 "$ADDP_ONLINE_SECRET_DIR/postgres-before.json"
for role in namenode datanode; do
  docker run -d --name "addp-hdfs-online-$role" --label "$LABEL=$SUITE" --network host --user root \
    --env HDFS_RPC_URI=hdfs://127.0.0.1:8020 --env HDFS_DATANODE_HOST=127.0.0.1 --env HDFS_LIST_LIMIT=2 \
    --mount "type=bind,source=$ROOT_DIR/business/hdfs,target=/addp/hdfs,readonly" \
    --tmpfs /data:mode=1777 --memory 1g --entrypoint bash "$HADOOP_IMAGE" /addp/hdfs/start.sh "$role" >/dev/null
done
docker run -d --name addp-hdfs-online-master --label "$LABEL=$SUITE" --network host --user root --memory 2g \
  --env HDFS_RPC_URI=hdfs://127.0.0.1:8020 \
  --mount "type=bind,source=$ROOT_DIR/business/hdfs,target=/addp/hdfs,readonly" \
  --entrypoint bash "$SPARK_IMAGE" -c '
    set -e
    /opt/spark/sbin/start-master.sh --host 127.0.0.1 --port 7077 --webui-port 18080
    /opt/spark/sbin/start-thriftserver.sh --master spark://127.0.0.1:7077 --conf spark.driver.host=127.0.0.1 --conf spark.driver.bindAddress=127.0.0.1 --conf spark.cores.max=1 --conf spark.executor.cores=1 --conf spark.executor.memory=1g
    exec tail -f /dev/null' >/dev/null
docker run -d --name addp-hdfs-online-worker --label "$LABEL=$SUITE" --network host --user root --memory 3g \
  --add-host host.docker.internal:127.0.0.1 \
  --entrypoint /opt/spark/bin/spark-class "$SPARK_IMAGE" org.apache.spark.deploy.worker.Worker \
    --host 127.0.0.1 --cores 2 --memory 2g --webui-port 18081 spark://127.0.0.1:7077 >/dev/null
docker exec --env HADOOP_USER_NAME=root addp-hdfs-online-master /opt/spark/bin/spark-submit /addp/hdfs/init.py
python3 - <<'PY'
import json, os, time, urllib.request
from pathlib import Path
deadline = time.monotonic() + 120
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen('http://127.0.0.1:18080/json', timeout=5) as response:
            master = json.load(response)
        if master.get('aliveworkers') == 1 and any(app.get('name') == 'Thrift JDBC/ODBC Server' and app.get('state') == 'RUNNING' for app in master.get('activeapps', [])):
            break
    except (OSError, ValueError):
        pass
    time.sleep(1)
else:
    raise SystemExit('Spark Standalone readiness timed out')
root = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
for name, engine_type, connection in (
    ('hdfs', 'hdfs', {'webhdfs_endpoint': 'http://127.0.0.1:9870', 'rpc_uri': 'hdfs://127.0.0.1:8020',
                      'root_path': '/addp', 'authentication': 'simple', 'user': 'addp_business_reader'}),
    ('spark', 'spark', {'host': '127.0.0.1', 'port': 10000, 'master_port': 7077, 'database': 'default', 'username': 'spark'}),
):
    value = {'name': 'Hosted ' + name.upper(), 'engine_type': engine_type, 'engine_origin': 'general',
             'description': 'Disposable HDFS distributed Spark acceptance', 'connection_info': connection}
    descriptor = os.open(root / (name + '-engine.json'), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream)
PY
echo 'Disposable Business HDFS and Spark samples ready'
