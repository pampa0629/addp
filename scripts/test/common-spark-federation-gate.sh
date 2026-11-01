#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=postgres,mysql,spark-master,spark-worker
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.spark-federation-t2.yml
# ADDP_T2_INPUT_FILES=engines/spark-workflow/ common/engine/plugins/spark/ scripts/test/spark-federation-contract.py
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
    compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
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
CREATE USER reader WITH PASSWORD 'admission-reader-only';
GRANT CONNECT ON DATABASE addp_test TO reader;
GRANT USAGE ON SCHEMA public TO reader;
GRANT SELECT ON orders TO reader;
SQL
compose exec -T mysql mysql --default-character-set=utf8mb4 -uroot -powned-test-only addp_test <<'SQL'
CREATE TABLE customers (id int PRIMARY KEY, label varchar(64));
INSERT INTO customers VALUES (1,'中文  '),(2,NULL);
CREATE USER 'reader'@'%' IDENTIFIED BY 'admission-reader-only';
GRANT SELECT ON addp_test.customers TO 'reader'@'%';
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
echo 'SPARK_FEDERATION_ADMISSION_PASS native_query_and_fresh_driver_only'
