#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=doris
# ADDP_T2_INPUT_FILES=engines/spark-workflow/ common/engine/plugins/doris/ business/doris/ scripts/test/spark-table-publication-contract.py scripts/test/common-spark-table-gate_test.py
# Full gate owns disposable Linux x86_64 databases and a real Spark Worker.
# The explicit mysql subset is available on multi-architecture Docker hosts.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
test_engine=${SPARK_TABLE_TEST_ENGINE:-all}
case "$test_engine" in all|mysql|doris) ;; *) echo 'SPARK_TABLE_TEST_ENGINE must be all, mysql or doris' >&2; exit 2;; esac
if [ "$test_engine" != mysql ]; then
    case "$(docker info --format '{{.Architecture}}')" in
        x86_64|amd64) ;;
        *) echo 'Full Doris publication T2 requires native Linux x86_64 Docker; SPARK_TABLE_TEST_ENGINE=mysql runs only the MySQL subset' >&2; exit 1;;
    esac
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-spark-table.XXXXXX")
OWNER="addp-spark-table-t2-${PPID}-$$"
MYSQL="$OWNER-mysql" DORIS="$OWNER-doris" MASTER="$OWNER-master" WORKER="$OWNER-worker"
SPARK_IMAGE='apache/spark:3.5.0-scala2.12-java11-python3-ubuntu@sha256:0ed5154e6b32ac3af1272d4d65e9f65b13afcfe80b41ad10bd059bcd6317863c'
MYSQL_IMAGE='mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b'
DORIS_IMAGE='apache/doris:doris-all-in-one-2.1.0@sha256:83538f071c8ea22134b8151d8b751cdf36da9e1a5da315443ba461909ee11835'
cleanup() {
    local status=$? container containers networks volume volumes=''
    trap - EXIT INT TERM
    set +e
    for container in "$WORKER" "$MASTER" "$DORIS" "$MYSQL"; do
        if docker container inspect "$container" >/dev/null 2>&1; then
            volumes="$volumes $(docker inspect --format '{{range .Mounts}}{{if eq .Type "volume"}}{{println .Name}}{{end}}{{end}}' "$container")"
            if [ "$status" -ne 0 ]; then docker logs --tail 60 "$container" >&2; fi
            docker rm --force --volumes "$container" >/dev/null 2>&1 || status=1
        fi
    done
    for volume in $volumes; do
        if docker volume inspect "$volume" >/dev/null 2>&1; then
            echo "Spark table T2 left owned volume: $volume" >&2; status=1
        fi
    done
    if docker network inspect "$OWNER" >/dev/null 2>&1; then docker network rm "$OWNER" >/dev/null 2>&1 || status=1; fi
    containers=$(docker ps -aq --filter "label=addp.spark.table.owner=$OWNER") || status=1
    networks=$(docker network ls -q --filter "label=addp.spark.table.owner=$OWNER") || status=1
    if [ -n "$containers$networks" ]; then echo 'Spark table T2 left owned resources' >&2; status=1; fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 - "$ROOT_DIR" "$WORK_DIR" <<'PY'
import sys
sys.path.insert(0, sys.argv[1] + '/engines/spark-workflow')
from spark_dependencies import ensure_doris_jar
print('Verified Doris connector:', ensure_doris_jar(sys.argv[2]).name)
PY
docker network create --label "addp.spark.table.owner=$OWNER" "$OWNER" >/dev/null
docker run -d --name "$MYSQL" --label "addp.spark.table.owner=$OWNER" --network "$OWNER" --network-alias mysql \
    --tmpfs /var/lib/mysql --memory 1g -e MYSQL_ROOT_PASSWORD=owned-spark-only -e MYSQL_DATABASE=spark_results "$MYSQL_IMAGE" >/dev/null
if [ "$test_engine" != mysql ]; then
DORIS_BOOTSTRAP=$(cat <<'BOOTSTRAP'
set -euo pipefail
sed -i 's/-Xmx8192m/-Xmx1024m/g; s/-Xms8192m/-Xms1024m/g' /opt/apache-doris/fe/conf/fe.conf
echo 'mem_limit = 1024m' >> /opt/apache-doris/be/conf/be.conf
exec bash /addp/doris/init.sh
BOOTSTRAP
)
    docker run -d --name "$DORIS" --label "addp.spark.table.owner=$OWNER" --network "$OWNER" --network-alias doris \
        --network-alias host.docker.internal --add-host host.docker.internal:127.0.0.1 \
        -v "$ROOT_DIR/business/doris/init.sh:/addp/doris/init.sh:ro" \
        --hostname doris --memory 3g -e BUILD_TYPE=production --entrypoint bash "$DORIS_IMAGE" -c "$DORIS_BOOTSTRAP" >/dev/null
fi
docker run -d --name "$MASTER" --label "addp.spark.table.owner=$OWNER" --network "$OWNER" --network-alias spark-master --user root --memory 2g \
    -v "$ROOT_DIR/engines/spark-workflow:/addp/spark-workflow:ro" -v "$ROOT_DIR/scripts/test/spark-table-publication-contract.py:/addp/contract.py:ro" -v "$WORK_DIR:/gate" \
    --entrypoint /opt/spark/bin/spark-class "$SPARK_IMAGE" org.apache.spark.deploy.master.Master --host spark-master --port 7077 >/dev/null
docker run -d --name "$WORKER" --label "addp.spark.table.owner=$OWNER" --network "$OWNER" --network-alias spark-worker --user root --memory 2g \
    --entrypoint /opt/spark/bin/spark-class "$SPARK_IMAGE" org.apache.spark.deploy.worker.Worker --host spark-worker --cores 2 --memory 1g spark://spark-master:7077 >/dev/null
ready=false
for _ in $(seq 1 120); do
    if ! docker exec "$MYSQL" mysql -h127.0.0.1 -uroot -powned-spark-only --connect-timeout=2 -e 'SELECT 1' >/dev/null 2>&1; then sleep 2; continue; fi
    if [ "$test_engine" != mysql ]; then
        if [ "$(docker inspect --format '{{.State.Running}}' "$DORIS")" != true ]; then echo 'Doris disposable container exited before readiness' >&2; exit 1; fi
        if ! docker exec "$DORIS" mysql -h127.0.0.1 -P9030 -uroot --batch -e 'SHOW BACKENDS;' 2>/dev/null |
            awk -F '\t' 'NR == 1 { for (i=1;i<=NF;i++) if ($i=="Alive") alive=i; next } alive && tolower($alive)=="true" { found=1 } END { exit !found }'; then sleep 5; continue; fi
        # Exercise the real entrypoint for a new cluster, then migrate a legacy
        # loopback identity in place, keeping the same Backend throughout.
        docker exec -i "$DORIS" bash <<'VERIFY'
set -euo pipefail
source /addp/doris/init.sh
before=$(doris_backend_facts)
before_id=${before%%$'\t'*}
doris_sql -e 'ALTER SYSTEM MODIFY BACKEND "host.docker.internal:9050" HOSTNAME "127.0.0.1";'
doris_register_backend
after=$(doris_backend_facts)
[ "${after%%$'\t'*}" = "$before_id" ]
for _ in {1..30}; do
    if doris_health; then exit 0; fi
    sleep 1
done
exit 1
VERIFY
        docker exec "$DORIS" mysql -h127.0.0.1 -P9030 -uroot -e 'CREATE DATABASE spark_results;'
    fi
    ready=true; break
done
[ "$ready" = true ] || { echo 'Disposable databases not ready' >&2; exit 1; }
docker exec "$MASTER" /opt/spark/bin/spark-submit --master spark://spark-master:7077 \
    --conf spark.driver.host=spark-master --conf spark.task.maxFailures=1 \
    --packages com.mysql:mysql-connector-j:8.4.0 --jars /gate/spark-doris-connector-spark-3.5-25.2.0.jar \
    /addp/contract.py "$test_engine" 2>&1 | tee "$WORK_DIR/contract.log"
if [ "$test_engine" != doris ]; then grep -q '^SPARK_MYSQL_PUBLICATION_PASS ' "$WORK_DIR/contract.log"; fi
if [ "$test_engine" != mysql ]; then grep -q '^SPARK_DORIS_PUBLICATION_PASS ' "$WORK_DIR/contract.log"; fi
echo "SPARK_TABLE_T2_PASS engines=$test_engine staged publication; real Worker"
