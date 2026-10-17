#!/usr/bin/env bash
# HDFS catalog, scan, preview and formal distributed Spark Console acceptance.
# ADDP_ONLINE_SUITES=hdfs-spark-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=hdfs-spark-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-hdfs-online-namenode addp-hdfs-online-datanode addp-hdfs-online-master addp-hdfs-online-worker addp-hdfs-online-postgres addp-hdfs-online-minio addp-hdfs-online-mysql addp-hdfs-online-doris addp-hdfs-online-runtime addp-hdfs-online-registry)
RUNTIME_CONTAINER=addp-hdfs-online-runtime
RUNTIME_IMAGE=localhost:5001/addp-spark-workflow-engine:online-hdfs
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim-bookworm localhost:5001/eclipse-temurin:11-jre-jammy)
HOSTED_SPARK_REGISTRY=addp-hdfs-online-registry
table_publication_started=0
stop_online_fixture() {
  local status=0 image
  # Also run domain cleanup after a timeout/signal killed the scenario process.
  if [ "$table_publication_started" -eq 1 ]; then
    run_logged python3 scripts/test/hdfs-spark-consumer-flow-online.py --cleanup-tables || status=1
  fi
  run_logged bash business/scripts/online-hdfs-spark-fixture.sh stop || status=1
  stop_online_spark_image_registry || status=1
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170
export SECURITY_URL=http://127.0.0.1:8194
export HADOOP_USER_NAME=addp_business_reader SPARK_WORKFLOW_SHARED_HOST=127.0.0.1
export ADDP_ONLINE_TEST_RUN_ID="run-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
# Explicitly refuse inherited local-mode settings; real Worker evidence is mandatory.
[ -z "${SPARK_MODE:-}" ] || fail 'SPARK_MODE override is forbidden'
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-hdfs-spark-fixture.sh start
application_owned=1
for start_target in -security -meta -manager -develop; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_spark_image_registry
start_online_spark_runtime online-hdfs hdfs
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite hdfs-spark-consumer-flow --wait-protection-owners develop,manager --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
for engine in hdfs spark postgres minio mysql doris; do
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/$engine-engine.json" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  if [ "$engine" = hdfs ]; then
    export ADDP_ONLINE_HDFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  elif [ "$engine" = postgres ]; then
    export ADDP_ONLINE_POSTGRES_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  elif [ "$engine" = minio ]; then
    export ADDP_ONLINE_MINIO_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  elif [ "$engine" = mysql ]; then
    export ADDP_ONLINE_MYSQL_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  elif [ "$engine" = doris ]; then
    export ADDP_ONLINE_DORIS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  else
    export ADDP_ONLINE_SPARK_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  fi
done
table_publication_started=1
run_logged python3 scripts/test/hdfs-spark-consumer-flow-online.py --initialize-table-read
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
