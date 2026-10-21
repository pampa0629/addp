#!/usr/bin/env bash
# Three real Runtime readiness and registration faults on disposable Hosted Linux.
# ADDP_ONLINE_SUITES=workflow-runtime-health-recovery
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
ONLINE_SUITE=workflow-runtime-health-recovery
HOSTED_FIXTURE_CONTAINERS=(addp-health-pointcloud addp-health-model3d addp-health-document addp-health-registry)
HOSTED_FIXTURE_IMAGES=(127.0.0.1:5001/addp-model3d-converter:online 127.0.0.1:5001/addp-model3d-workflow-engine:online
  127.0.0.1:5001/addp-pointcloud-workflow-engine:online 127.0.0.1:5001/addp-document-workflow-engine:online
  127.0.0.1:5001/addp-pointcloud-workflow-engine:startup-missing
  127.0.0.1:5001/addp-model3d-workflow-engine:startup-missing
  127.0.0.1:5001/addp-document-workflow-engine:startup-missing)
stop_online_fixture() {
  local status=0 name image
  for name in pointcloud model3d document registry; do
    if docker container inspect "addp-health-$name" >/dev/null 2>&1; then
      if [ "$name" != registry ]; then
        [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-runtime" }}' "addp-health-$name")" = "$ONLINE_SUITE" ] || { status=1; continue; }
        run_logged docker logs "addp-health-$name" > "$ADDP_ONLINE_ARTIFACT_DIR/$name-runtime.log" || status=1
      fi
      [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-run-id" }}' "addp-health-$name")" = "$ADDP_ONLINE_TEST_RUN_ID" ] || { status=1; continue; }
      run_logged docker rm -fv "addp-health-$name" || status=1
      if docker container inspect "addp-health-$name" >/dev/null 2>&1; then status=1; fi
    fi
  done
  for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      run_logged docker image rm "$image" || status=1
      if docker image inspect "$image" >/dev/null 2>&1; then status=1; fi
    fi
  done
  remaining=$(docker ps -aq --filter "label=com.addp.online-run-id=$ADDP_ONLINE_TEST_RUN_ID") || status=1
  [ -z "$remaining" ] || status=1
  printf 'runtime_cleanup=%s\n' "$([ "$status" -eq 0 ] && echo zero_residuals || echo failed)" > "$ADDP_ONLINE_ARTIFACT_DIR/runtime-cleanup.txt"
  return "$status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export ADDP_ONLINE_TEST_RUN_ID="health-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export ADDP_OBSERVABILITY_METRICS_ENABLED=false ADDP_OBSERVABILITY_LOGS_ENABLED=false ADDP_OBSERVABILITY_TRACES_ENABLED=false
for key in DEVELOP_SERVICE_CLIENT_SECRET POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET DOCUMENT_WORKFLOW_SERVICE_CLIENT_SECRET; do
  printf -v "$key" '%s' "$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
  export "$key"
done
# Restore before stopping System as well as from the scenario's finally path.
stop_online_application() {
  local status=0
  run_logged python3 scripts/test/workflow-runtime-health-recovery-online.py --restore-only || status=1
  run_logged bash scripts/dev/stop.sh || status=1
  return "$status"
}
fixture_owned=1
run_logged docker run -d --name addp-health-registry --label "com.addp.online-run-id=$ADDP_ONLINE_TEST_RUN_ID" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'temporary image registry did not become ready'
  sleep 1
done
run_logged make build-images IMAGE_BUILD_ARGS='--verify --jobs 1 --registry 127.0.0.1:5001 --tag online --services model3d-workflow-engine,pointcloud-workflow-engine,document-workflow-engine'
infra_owned=1
run_logged bash scripts/infra/up.sh
application_owned=1
for target in -system -gateway; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process "$target"
done
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite workflow-runtime-health-recovery --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 -m venv "$ADDP_ONLINE_SECRET_DIR/observer"
run_logged "$ADDP_ONLINE_SECRET_DIR/observer/bin/python" -m pip install ./common-python
run_logged "$ADDP_ONLINE_SECRET_DIR/observer/bin/python" scripts/test/workflow-runtime-health-recovery-online.py --assert-empty
context="$ADDP_ONLINE_SECRET_DIR/startup-image"
mkdir "$context"
cp scripts/test/workflow-runtime-health-tool.py "$context/agent.py"
cat > "$context/Dockerfile" <<'DOCKERFILE'
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
ARG TOOL_ENV
ARG RUNTIME_USER
USER 0
COPY agent.py /tmp/addp-health-image-agent.py
RUN python /tmp/addp-health-image-agent.py image-missing "$TOOL_ENV" && rm /tmp/addp-health-image-agent.py
USER ${RUNTIME_USER}
DOCKERFILE
for name in pointcloud model3d document; do
  RUNTIME_CONTAINER="addp-health-$name"
  product_image="127.0.0.1:5001/addp-$name-workflow-engine:online"
  RUNTIME_IMAGE="127.0.0.1:5001/addp-$name-workflow-engine:startup-missing"
  RUNTIME_REPORT=$name
  upper=$(printf '%s' "$name" | tr '[:lower:]' '[:upper:]')
  key="${upper}_WORKFLOW_PORT"
  port=${!key}
  secret="${upper}_WORKFLOW_SERVICE_CLIENT_SECRET"
  case "$name" in
    pointcloud) tool_env=POINTCLOUD_PDAL_BIN ;;
    model3d) tool_env=MODEL3D_IFC_CONVERTER_BIN ;;
    document) tool_env=DOCUMENT_LIBREOFFICE_BIN ;;
  esac
  runtime_user=$(docker image inspect -f '{{.Config.User}}' "$product_image")
  run_logged docker build --build-arg "BASE_IMAGE=$product_image" --build-arg "TOOL_ENV=$tool_env" \
    --build-arg "RUNTIME_USER=${runtime_user:-0}" --tag "$RUNTIME_IMAGE" "$context"
  run_logged docker run -d --name "$RUNTIME_CONTAINER" --network host \
    --label "com.addp.online-runtime=$ONLINE_SUITE" --label "com.addp.online-run-id=$ADDP_ONLINE_TEST_RUN_ID" \
    -e "PORT=$port" -e WORKFLOW_BIND_HOST=127.0.0.1 -e SYSTEM_URL -e "$secret" -e RUNTIME_HOST=127.0.0.1 "$RUNTIME_IMAGE"
  verify_online_product_identity '["python","api_server.py"]'
  printf 'product_image_id=%s\nstartup_missing=true\n' "$(docker image inspect -f '{{.Id}}' "$product_image")" \
    >> "$ADDP_ONLINE_ARTIFACT_DIR/$name-runtime-build.txt"
done
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
