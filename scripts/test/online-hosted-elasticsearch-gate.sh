#!/usr/bin/env bash
# Elasticsearch catalog, scan, document preview and DSL Console acceptance on GitHub Hosted.
# ADDP_ONLINE_SUITES=elasticsearch-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=elasticsearch-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-elasticsearch-online-disposable addp-elasticsearch-online-master addp-elasticsearch-online-worker addp-elasticsearch-online-registry spark-workflow-engine)
HOSTED_SPARK_REGISTRY=addp-elasticsearch-online-registry
RUNTIME_IMAGE=localhost:5001/addp-spark-workflow-engine:online-elasticsearch
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim-bookworm localhost:5001/eclipse-temurin:11-jre-jammy)
stop_online_fixture() {
  local status=0
  run_logged bash business/scripts/online-elasticsearch-consumer-fixture.sh stop || status=1
  stop_online_spark_image_registry || status=1
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export CONSOLE_URL=http://127.0.0.1:5170
export SPARK_WORKFLOW_SHARED_HOST=127.0.0.1 REGISTRY=localhost:5001 IMAGE_TAG=online-elasticsearch
[ -z "${SPARK_MODE:-}" ] || fail 'SPARK_MODE override is forbidden'
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/elasticsearch-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-elasticsearch-consumer-fixture.sh start
start_online_spark_image_registry
application_owned=1
for start_target in -meta -manager -develop -spark-workflow; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
image_id=$(docker image inspect -f '{{.Id}}' "$RUNTIME_IMAGE")
[ "$(docker inspect -f '{{.Image}}' spark-workflow-engine)" = "$image_id" ] || fail 'Runtime image identity mismatch'
[ "$(docker inspect -f '{{json .Config.Cmd}}' spark-workflow-engine)" = '["python","api_server.py"]' ] || fail 'Runtime default entry mismatch'
printf 'image_id=%s\ngit_commit=%s\ndefault_entry=python api_server.py\n' "$image_id" "$(git rev-parse HEAD)" > "$ADDP_ONLINE_ARTIFACT_DIR/elasticsearch-runtime-build.txt"
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite elasticsearch-consumer-flow --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_ELASTICSEARCH_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/spark-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_SPARK_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
export ADDP_ONLINE_SPARK_RUNTIME_URL="http://127.0.0.1:$SPARK_WORKFLOW_PORT"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
