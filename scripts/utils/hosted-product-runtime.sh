#!/usr/bin/env bash
# Single product launch and identity checks shared by Hosted lifecycle and capacity restarts.
start_online_geopython_runtime() {
  local tag=$1
  shift
  run_logged make build-images IMAGE_BUILD_ARGS="--services geopython-workflow-engine --tag $tag --verify --jobs 1"
  launch_online_geopython_runtime "$@"
}

launch_online_geopython_runtime() {
  RUNTIME_REPORT=raster
  runtime_owned=1
  run_logged docker run -d --name "$RUNTIME_CONTAINER" --network host \
    --label "com.addp.online-runtime=$ONLINE_SUITE" \
    --label "com.addp.online-run-id=$ADDP_ONLINE_TEST_RUN_ID" \
    -e "PORT=$GEOPYTHON_WORKFLOW_PORT" -e WORKFLOW_BIND_HOST=127.0.0.1 \
    -e SYSTEM_URL -e GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET -e RUNTIME_HOST=127.0.0.1 \
    -e GEOPYTHON_WORKFLOW_LOOPBACK_HOST=127.0.0.1 "$@" "$RUNTIME_IMAGE"
  verify_online_product_runtime '["sh","container_entrypoint.sh"]' "http://127.0.0.1:$GEOPYTHON_WORKFLOW_PORT/health"
}

start_online_model3d_runtime() (
  RUNTIME_CONTAINER=$1
  RUNTIME_IMAGE=$2
  RUNTIME_REPORT=model3d
  run_logged docker run -d --name "$RUNTIME_CONTAINER" --network host \
    --label "com.addp.online-runtime=$ONLINE_SUITE" \
    --label "com.addp.online-run-id=$ADDP_ONLINE_TEST_RUN_ID" \
    -e "PORT=$MODEL3D_WORKFLOW_PORT" -e WORKFLOW_BIND_HOST=127.0.0.1 \
    -e SYSTEM_URL -e MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET -e RUNTIME_HOST=127.0.0.1 "$RUNTIME_IMAGE"
  verify_online_product_runtime '["python","api_server.py"]' "http://127.0.0.1:$MODEL3D_WORKFLOW_PORT/health"
)

verify_online_product_runtime() {
  local command=$1 health_url=$2 image_id attempt default_entry
  image_id=$(docker image inspect -f '{{.Id}}' "$RUNTIME_IMAGE")
  [ "$(docker inspect -f '{{.Image}}' "$RUNTIME_CONTAINER")" = "$image_id" ] || fail 'Runtime image identity mismatch'
  [ "$(docker inspect -f '{{json .Config.Cmd}}' "$RUNTIME_CONTAINER")" = "$command" ] || fail 'Runtime default entry mismatch'
  default_entry=$(python3 -c 'import json,sys; print(" ".join(json.loads(sys.argv[1])))' "$command")
  printf 'image_id=%s\ngit_commit=%s\ndefault_entry=%s\n' "$image_id" "$(git rev-parse HEAD)" "$default_entry" > "$ADDP_ONLINE_ARTIFACT_DIR/${RUNTIME_REPORT}-runtime-build.txt"
  for attempt in $(seq 1 120); do
    if curl -fsS "$health_url" >/dev/null; then break; fi
    [ "$attempt" -lt 120 ] || fail 'Runtime readiness timed out'
    sleep 1
  done
  [ "$(docker inspect -f '{{.State.Running}}' "$RUNTIME_CONTAINER")" = true ] || fail 'Runtime container exited'
}
