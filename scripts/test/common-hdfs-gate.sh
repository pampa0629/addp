#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=hdfs-namenode,hdfs-datanode,spark-master,spark-worker,postgres,postgres-amd64,minio,minio-other
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.hdfs-t2.yml
# ADDP_T2_INPUT_FILES=business/hdfs/ business/docker-compose.yml scripts/infra/Dockerfile.minio engines/spark-workflow/ develop/backend/internal/service/workflow_engine_service.go develop/backend/internal/service/workflow_operator_adapter.go develop/backend/internal/service/workflow_execution_authorization.go develop/backend/internal/service/dev_executor.go meta/backend/internal/metaenrich/table_file.go scripts/test/hdfs-spark-contract.py
# Own disposable containers, volumes and network; never reuse Business services.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export HDFS_T2_WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-hdfs.XXXXXX")
COMPOSE_PROJECT="addp-hdfs-t2-${PPID}-$$"
export HDFS_MINIO_TEST_IMAGE="$COMPOSE_PROJECT-minio:RELEASE.2025-10-15T17-29-55Z"
compose() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.hdfs-t2.yml" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    if [ "$status" -ne 0 ]; then compose logs --tail 30 >&2; fi
    compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    if docker ps -a --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q . || docker volume ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.Name}}' | grep -q . || docker network ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q .; then
        echo "HDFS T2 cleanup left resources" >&2; status=1
    fi
    if docker image inspect "$HDFS_MINIO_TEST_IMAGE" >/dev/null 2>&1; then
        docker image rm "$HDFS_MINIO_TEST_IMAGE" >/dev/null 2>&1 || status=1
    fi
    rm -rf "$HDFS_T2_WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) arch=arm64; POSTGRES_SERVICE=postgres;; x86_64|amd64) arch=amd64; POSTGRES_SERVICE=postgres-amd64;; *) echo "Unsupported Docker architecture" >&2; exit 1;; esac
(cd "$ROOT_DIR/common" && GOWORK=off GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go test -c ./engine/plugins/hdfs -o "$HDFS_T2_WORK_DIR/hdfs.test")
mkdir "$HDFS_T2_WORK_DIR/samples"
compose build minio
compose up -d --wait --wait-timeout 180 "$POSTGRES_SERVICE" minio minio-other hdfs-namenode hdfs-datanode spark-master spark-worker
for store in minio minio-other; do
    compose exec -T "$store" sh -c 'mc alias set owned http://localhost:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null && mc mb owned/result >/dev/null'
done
# Readiness is checked by the shared initializer; two invocations prove idempotence.
for iteration in 1 2; do
    compose exec -T -e HADOOP_USER_NAME=root spark-master /opt/spark/bin/spark-submit /addp/hdfs/init.py 2>&1 | tee "$HDFS_T2_WORK_DIR/sample-$iteration.log"
    grep -q '^HDFS_SAMPLE_PASS ' "$HDFS_T2_WORK_DIR/sample-$iteration.log"
done
compose exec -T -e ADDP_HDFS_INTEGRATION=1 -e ADDP_HDFS_SAMPLE_DIR=/gate/samples hdfs-namenode /gate/hdfs.test -test.run '^TestIntegrationHDFS$' -test.v 2>&1 | tee "$HDFS_T2_WORK_DIR/go.log"
if grep -q -- '--- SKIP:' "$HDFS_T2_WORK_DIR/go.log"; then echo "HDFS T2 refuses skipped contracts" >&2; exit 1; fi
grep -q -- '--- PASS: TestIntegrationHDFS' "$HDFS_T2_WORK_DIR/go.log"
(cd "$ROOT_DIR/common" && GOWORK=off ADDP_HDFS_INTEGRATION=1 ADDP_HDFS_SAMPLE_DIR="$HDFS_T2_WORK_DIR/samples" go test -tags hdfs_formats ./engine/plugins/hdfs -run '^TestIntegrationHDFSFormats$' -count=1 -v) 2>&1 | tee "$HDFS_T2_WORK_DIR/formats.log"
if grep -q -- '--- SKIP:' "$HDFS_T2_WORK_DIR/formats.log"; then echo "HDFS T2 refuses skipped format consumers" >&2; exit 1; fi
grep -q -- '--- PASS: TestIntegrationHDFSFormats' "$HDFS_T2_WORK_DIR/formats.log"

compose exec -T spark-master pip install --disable-pip-version-check apache-sedona==1.5.3
compose exec -T -e HADOOP_USER_NAME=addp_business_reader spark-master /opt/spark/bin/spark-submit --master spark://spark-master:7077 --packages org.postgresql:postgresql:42.7.4,org.apache.sedona:sedona-spark-shaded-3.5_2.12:1.5.3,org.datasyslab:geotools-wrapper:1.5.3-28.2,org.apache.spark:spark-hadoop-cloud_2.12:3.5.0 --exclude-packages com.google.cloud.bigdataoss:gcs-connector,org.apache.hadoop:hadoop-azure,org.apache.hadoop:hadoop-cloud-storage /addp/hdfs-spark-contract.py 2>&1 | tee "$HDFS_T2_WORK_DIR/spark.log"
grep -q '^HDFS_SPARK_PASS formats=csv,json,parquet distributed=true$' "$HDFS_T2_WORK_DIR/spark.log"
grep -q '^HDFS_SPARK_POSTGRES_PASS ' "$HDFS_T2_WORK_DIR/spark.log"
grep -q '^SPARK_MINIO_PARQUET_PASS ' "$HDFS_T2_WORK_DIR/spark.log"
echo "HDFS_T2_PASS WebHDFS, distributed Spark, PostgreSQL and MinIO publication contracts"
