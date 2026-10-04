#!/usr/bin/env bash
# HDFS catalog, scan, preview and formal distributed Spark Console acceptance.
# ADDP_ONLINE_SUITES=hdfs-spark-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=hdfs-spark-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-hdfs-online-namenode addp-hdfs-online-datanode addp-hdfs-online-master addp-hdfs-online-worker)
stop_online_fixture() {
  run_logged bash business/scripts/online-hdfs-spark-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170
export HADOOP_USER_NAME=addp_business_reader SPARK_WORKFLOW_SHARED_HOST=127.0.0.1
# Explicitly refuse inherited local-mode settings; real Worker evidence is mandatory.
[ -z "${SPARK_MODE:-}" ] || fail 'SPARK_MODE override is forbidden'
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-hdfs-spark-fixture.sh start
application_owned=1
for start_target in -meta -manager -develop -spark-workflow; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite hdfs-spark-consumer-flow --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
for engine in hdfs spark; do
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/$engine-engine.json" --output "$ENGINE_RESULT_ENV"
  source "$ENGINE_RESULT_ENV"
  if [ "$engine" = hdfs ]; then
    export ADDP_ONLINE_HDFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  else
    export ADDP_ONLINE_SPARK_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
  fi
done
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
