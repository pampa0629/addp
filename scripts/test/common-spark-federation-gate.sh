#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=postgres,mysql,spark-master,spark-worker,spark-master-b,spark-worker-b,driver
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.spark-federation-t2.yml
# ADDP_T2_INPUT_FILES=engines/spark-workflow/ common-python/ common/engine/plugins/spark/ common/engine/plugin/http_prepared_query.go common/engine/plugin/federated_prepared_query.go scripts/test/spark-federation-contract.py
# Native admission only: disposable services, no personal Business or Infra access.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PROJECT="addp-spark-federation-${PPID}-$$"
PROBE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-spark-probe.XXXXXX")
compose() { docker compose -p "$PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.spark-federation-t2.yml" "$@"; }
cleanup() {
    local status=$? containers volumes networks
    trap - EXIT INT TERM
    set +e
    if ! compose down --volumes --remove-orphans >"$PROBE_DIR/cleanup.log" 2>&1; then
        cat "$PROBE_DIR/cleanup.log" >&2
        status=1
    fi
    containers=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT") || status=1
    volumes=$(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT") || status=1
    networks=$(docker network ls -q --filter "label=com.docker.compose.project=$PROJECT") || status=1
    if [ -n "$containers$volumes$networks" ]; then
        echo 'Spark federation admission left owned resources' >&2
        status=1
    fi
    rm -rf "$PROBE_DIR" || status=1
    [ "$status" -ne 0 ] || echo 'SPARK_FEDERATION_CLEANUP_PASS zero_residuals'
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --wait --wait-timeout 180
# Real workflow Drivers use the same Python version and dependency inputs as
# the product. The Spark image's Python 3.8 remains only for the older probes.
dependency_proxy() {
    dependency_proxy_value=$1
    case "$1" in
        http://127.0.0.1:*|https://127.0.0.1:*) dependency_proxy_value=${1/127.0.0.1/host.docker.internal} ;;
        http://localhost:*|https://localhost:*) dependency_proxy_value=${1/localhost/host.docker.internal} ;;
    esac
}
driver_dependencies() {
    # Only dependency installation receives the inherited download proxy.
    # Query/workflow Driver execution never inherits HTTP_PROXY/HTTPS_PROXY.
    local plain_proxy tls_proxy dependency_proxy_value
    dependency_proxy "${HTTP_PROXY:-${http_proxy:-}}"; plain_proxy=$dependency_proxy_value
    dependency_proxy "${HTTPS_PROXY:-${https_proxy:-}}"; tls_proxy=$dependency_proxy_value
    HTTP_PROXY="$plain_proxy" http_proxy="$plain_proxy" HTTPS_PROXY="$tls_proxy" https_proxy="$tls_proxy" \
        compose exec -T -e HTTP_PROXY -e HTTPS_PROXY -e http_proxy -e https_proxy driver "$@"
}
driver_dependencies python -m pip install --disable-pip-version-check -r /addp/spark-workflow/requirements.txt
driver_dependencies sh -c 'mkdir -p /tmp/common-python && cp /addp/common-python/pyproject.toml /addp/common-python/README.md /tmp/common-python/ && cp -a /addp/common-python/addp_common /tmp/common-python/ && python -m pip install --disable-pip-version-check /tmp/common-python'
compose exec -T driver python -m pip check
# Match the product image's dependency preparation before timed invocations.
# This build-only local computation is not routing or real-Worker evidence.
driver_dependencies python - <<'PY'
from pyspark.sql import SparkSession
from spark_dependencies import configure_spark_dependencies
spark = configure_spark_dependencies(SparkSession.builder.master('local[1]')).getOrCreate()
try:
    assert spark.range(1).count() == 1
finally:
    spark.stop()
PY
# The fixed Spark runtime image contains a JRE. Compile Java 11 bytecode with
# the host build JDK against that exact image's JARs, then copy only the artifact.
mkdir -p "$PROBE_DIR/spark"
compose cp spark-master:/opt/spark/jars "$PROBE_DIR/spark/jars"
python3 "$ROOT_DIR/engines/spark-workflow/query_jdbc.py" build --spark-home "$PROBE_DIR/spark" --output "$PROBE_DIR/query-dialect.jar"
artifact_dir=$(compose exec -T spark-master python3 -c 'import sys; print(sys.prefix + "/share/addp-spark")' | tr -d '\r')
compose exec -T spark-master mkdir -p "$artifact_dir"
compose cp "$PROBE_DIR/query-dialect.jar" "spark-master:$artifact_dir/addp-spark-query-dialect.jar"
compose cp "$PROBE_DIR/query-dialect.json" "spark-master:$artifact_dir/addp-spark-query-dialect.json"
driver_artifacts=$(compose exec -T driver python -c 'import sys; print(sys.prefix + "/share/addp-spark")' | tr -d '\r')
compose exec -T driver mkdir -p "$driver_artifacts"
compose cp "$PROBE_DIR/query-dialect.jar" "driver:$driver_artifacts/addp-spark-query-dialect.jar"
compose cp "$PROBE_DIR/query-dialect.json" "driver:$driver_artifacts/addp-spark-query-dialect.json"

# Compile only the production probe tests for the container's actual Linux CPU.
# Arrow still needs Apache Thrift transitively; the removed Hive SQL client does not.
case "$(compose exec -T spark-master uname -m | tr -d '\r')" in
  aarch64|arm64) probe_arch=arm64 ;;
  x86_64) probe_arch=amd64 ;;
  *) echo 'Unsupported Spark probe architecture' >&2; exit 1 ;;
esac
(cd "$ROOT_DIR/common" && GOOS=linux GOARCH="$probe_arch" CGO_ENABLED=0 go test -c -o "$PROBE_DIR/spark-probe.test" ./engine/plugins/spark)
compose cp "$PROBE_DIR/spark-probe.test" spark-master:/tmp/addp-spark-probe.test
# Wait through the production state probe, never a page-only or TCP-only check.
probe_ready=false
for attempt in $(seq 1 30); do
  if compose exec -T --env ADDP_SPARK_NATIVE_PROBE=1 spark-master /tmp/addp-spark-probe.test -test.run '^TestNativeMasterProbeAgainstStandalone$' -test.v; then
    probe_ready=true
    break
  fi
  sleep 1
done
[ "$probe_ready" = true ] || { echo 'Native Spark Master probe did not become ready' >&2; exit 1; }
echo 'SPARK_MASTER_NATIVE_PROBE_PASS production_go actual_master worker no_thrift'
compose exec -T postgres psql -U fixture_admin -d addp_test -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE orders (id bigint PRIMARY KEY, customer_id integer, amount numeric(38,18));
INSERT INTO orders VALUES (9007199254740993,1,100.123456789012345678),(-9007199254740993,2,-200.987654321098765432);
CREATE SCHEMA " schema.with dot ";
CREATE TABLE " schema.with dot "." 中文""table` " AS SELECT * FROM orders;
ALTER TABLE " schema.with dot "." 中文""table` " RENAME COLUMN customer_id TO " customer""id ";
CREATE TABLE read_plan_parent (id integer PRIMARY KEY);
INSERT INTO read_plan_parent VALUES (1);
CREATE TABLE read_plan_child (id integer PRIMARY KEY);
INSERT INTO read_plan_child VALUES (999);
CREATE USER reader WITH PASSWORD 'admission-reader-only';
GRANT CONNECT ON DATABASE addp_test TO reader;
GRANT USAGE ON SCHEMA public TO reader;
GRANT SELECT ON orders, read_plan_parent TO reader;
GRANT USAGE ON SCHEMA " schema.with dot " TO reader;
GRANT SELECT ON " schema.with dot "." 中文""table` " TO reader;
SQL
compose exec -T mysql mysql --default-character-set=utf8mb4 -uroot -powned-test-only addp_test <<'SQL'
CREATE TABLE customers (id int PRIMARY KEY, label varchar(64));
INSERT INTO customers VALUES (1,'中文  '),(2,NULL);
CREATE TABLE `custom``ers` AS SELECT * FROM customers;
ALTER TABLE `custom``ers` CHANGE COLUMN id `customer``id` int, CHANGE COLUMN label `中文``label` varchar(64);
CREATE VIEW read_plan_view AS SELECT id FROM customers;
CREATE USER 'reader'@'%' IDENTIFIED BY 'admission-reader-only';
GRANT SELECT ON addp_test.customers TO 'reader'@'%';
GRANT SELECT ON addp_test.read_plan_view TO 'reader'@'%';
GRANT SELECT ON addp_test.`custom``ers` TO 'reader'@'%';
SQL
label_hex=$(compose exec -T mysql mysql -uroot -powned-test-only --batch --skip-column-names addp_test -e 'SELECT HEX(label) FROM customers WHERE id=1')
[ "$label_hex" = E4B8ADE696872020 ] || { echo "MySQL physical fixture bytes differ: $label_hex" >&2; exit 1; }
for phase in query guard fresh supervised driver; do
    compose exec -T spark-master /opt/spark/bin/spark-submit \
        --master spark://spark-master:7077 --conf spark.driver.host=spark-master \
        --conf spark.executor.memory=512m --conf spark.cores.max=2 \
        --conf spark.task.maxFailures=1 --conf spark.ui.enabled=false \
        --packages org.postgresql:postgresql:42.7.4,com.mysql:mysql-connector-j:8.4.0 \
        /addp/contract.py "$phase"
done
compose exec -T driver python /addp/contract.py multicluster
echo 'SPARK_FEDERATION_ADMISSION_PASS native_query_and_fresh_driver_only'
