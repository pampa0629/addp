#!/usr/bin/env bash
# HDFS catalog, scan, preview and formal distributed Spark Console acceptance.
# ADDP_ONLINE_SUITES=hdfs-spark-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=hdfs-spark-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-hdfs-online-namenode addp-hdfs-online-datanode addp-hdfs-online-master addp-hdfs-online-worker addp-hdfs-online-runtime addp-hdfs-online-registry)
RUNTIME_IMAGE=localhost:5001/addp-spark-workflow-engine:online-hdfs
HOSTED_FIXTURE_IMAGES=("$RUNTIME_IMAGE" localhost:5001/python:3.11-slim-bookworm localhost:5001/eclipse-temurin:11-jre-jammy)
registry_owned=0
runtime_owned=0
remove_owned_container() {
  local container=$1 status=0
  if docker container inspect "$container" >/dev/null 2>&1; then
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' "$container")" = "$ONLINE_SUITE" ] || return 1
    run_logged docker rm -fv "$container" || status=1
    if docker container inspect "$container" >/dev/null 2>&1; then status=1; fi
  fi
  return "$status"
}
stop_online_fixture() {
  local status=0 image
  run_logged bash business/scripts/online-hdfs-spark-fixture.sh stop || status=1
  if [ "$registry_owned" -eq 1 ]; then
    remove_owned_container addp-hdfs-online-registry || status=1
    for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
      if docker image inspect "$image" >/dev/null 2>&1; then
        run_logged docker image rm "$image" || status=1
        if docker image inspect "$image" >/dev/null 2>&1; then status=1; fi
      fi
    done
  fi
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
stop_online_application() {
  local status=0
  if [ "$runtime_owned" -eq 1 ]; then
    if docker container inspect addp-hdfs-online-runtime >/dev/null 2>&1 &&
      [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-fixture" }}' addp-hdfs-online-runtime)" = "$ONLINE_SUITE" ]; then
      run_logged docker logs addp-hdfs-online-runtime > "$ADDP_ONLINE_ARTIFACT_DIR/hdfs-runtime.log" || status=1
    fi
    remove_owned_container addp-hdfs-online-runtime || status=1
  fi
  run_logged bash scripts/dev/stop.sh || status=1
  return "$status"
}
export CONSOLE_URL=http://127.0.0.1:5170
export HADOOP_USER_NAME=addp_business_reader SPARK_WORKFLOW_SHARED_HOST=127.0.0.1
# Explicitly refuse inherited local-mode settings; real Worker evidence is mandatory.
[ -z "${SPARK_MODE:-}" ] || fail 'SPARK_MODE override is forbidden'
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-hdfs-spark-fixture.sh start
application_owned=1
for start_target in -meta -manager -develop; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
# The standard builder seeds its required base images into this owned mirror.
registry_owned=1
run_logged docker run -d --name addp-hdfs-online-registry --label "com.addp.online-fixture=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'image registry readiness timed out'
  sleep 1
done
run_logged make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --tag online-hdfs --verify --jobs 1"
runtime_owned=1
export ADDP_ONLINE_SPARK_RUNTIME_URL="http://127.0.0.1:$SPARK_WORKFLOW_PORT"
# No command override: exercise exactly the product image's default entry.
run_logged docker run -d --name addp-hdfs-online-runtime --network host \
  --label "com.addp.online-fixture=$ONLINE_SUITE" \
  -e "PORT=$SPARK_WORKFLOW_PORT" -e SYSTEM_URL -e SPARK_WORKFLOW_SERVICE_CLIENT_SECRET \
  -e HADOOP_USER_NAME -e SPARK_WORKFLOW_SHARED_HOST -e RUNTIME_HOST=127.0.0.1 "$RUNTIME_IMAGE"
image_id=$(docker image inspect -f '{{.Id}}' "$RUNTIME_IMAGE")
[ "$(docker inspect -f '{{.Image}}' addp-hdfs-online-runtime)" = "$image_id" ] || fail 'Runtime image identity mismatch'
[ "$(docker inspect -f '{{json .Config.Cmd}}' addp-hdfs-online-runtime)" = '["python","api_server.py"]' ] || fail 'Runtime default entry mismatch'
printf 'image_id=%s\ngit_commit=%s\ndefault_entry=python api_server.py\n' "$image_id" "$(git rev-parse HEAD)" > "$ADDP_ONLINE_ARTIFACT_DIR/hdfs-runtime-build.txt"
for attempt in $(seq 1 120); do
  if curl -fsS "$ADDP_ONLINE_SPARK_RUNTIME_URL/health" >/dev/null; then break; fi
  [ "$attempt" -lt 120 ] || fail 'Spark Runtime readiness timed out'
  sleep 1
done
[ "$(docker inspect -f '{{.State.Running}}' addp-hdfs-online-runtime)" = true ] || fail 'Runtime container exited'
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
