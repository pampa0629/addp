#!/usr/bin/env bash
# ADDP_ONLINE_SUITES=ftp-consumer-flow
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=ftp-consumer-flow
HOSTED_FIXTURE_CONTAINERS=(addp-ftp-model3d-runtime addp-ftp-online-registry addp-ftp-model-source)
HOSTED_FIXTURE_IMAGES=(127.0.0.1:5001/addp-model3d-converter:online 127.0.0.1:5001/addp-model3d-workflow-engine:online)
stop_online_fixture() {
  local cleanup_status=0 evidence image
  if docker container inspect addp-ftp-model3d-runtime >/dev/null 2>&1 &&
    [ "$(docker inspect -f '{{ index .Config.Labels "com.addp.online-runtime" }}' addp-ftp-model3d-runtime)" = "$ONLINE_SUITE" ]; then
    run_logged docker logs addp-ftp-model3d-runtime > "$ADDP_ONLINE_ARTIFACT_DIR/model3d-runtime.log" || cleanup_status=1
  fi
  remove_online_product_container addp-ftp-model3d-runtime || cleanup_status=1
  run_logged python3 -B -m scripts.test.online-file-model-nfs-fixture stop || cleanup_status=1
  run_logged python3 -B -m scripts.test.online-file-model-minio-fixture stop || cleanup_status=1
  [ ! -f "$ADDP_ONLINE_SECRET_DIR/minio-cleanup.json" ] || cp "$ADDP_ONLINE_SECRET_DIR/minio-cleanup.json" "$ADDP_ONLINE_ARTIFACT_DIR/minio-cleanup.json"
  run_logged python3 business/ftp/admission.py stop || cleanup_status=1
  for evidence in facts cleanup readiness-initial ftp-samples ftp-model-samples; do
    [ ! -f "$ADDP_ONLINE_SECRET_DIR/$evidence.json" ] || cp "$ADDP_ONLINE_SECRET_DIR/$evidence.json" "$ADDP_ONLINE_ARTIFACT_DIR/ftp-$evidence.json"
  done
  remove_online_product_container addp-ftp-online-registry || cleanup_status=1
  for image in "${HOSTED_FIXTURE_IMAGES[@]}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      run_logged docker image rm "$image" || cleanup_status=1
      if docker image inspect "$image" >/dev/null 2>&1; then cleanup_status=1; fi
    fi
  done
  return "$cleanup_status"
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
# Three sources exercise 24 models, including full grant/deny/revocation and
# task/result cleanup. Keep this suite's measured budget separate from the
# shared default and from every product/request execution timeout.
export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=1200
[ -n "${GITHUB_RUN_ID:-}" ] && [ -n "${GITHUB_RUN_ATTEMPT:-}" ] || fail 'GitHub run ID and attempt are required before Runtime startup'
export ADDP_ONLINE_TEST_RUN_ID="ftp-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
export MODEL3D_DOCKER_PLATFORM=linux/amd64
export ADDP_FTP_ADMISSION_DIR="$ADDP_ONLINE_SECRET_DIR"
export CONSOLE_URL=http://127.0.0.1:5170
export SECURITY_URL=http://127.0.0.1:8194
fixture_owned=1
run_logged docker run -d --name addp-ftp-online-registry \
  --label "com.addp.online-runtime=$ONLINE_SUITE" -p 127.0.0.1:5001:5000 registry:2
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:5001/v2/ >/dev/null; then break; fi
  [ "$attempt" -lt 30 ] || fail 'temporary image registry did not become ready'
  sleep 1
done
run_logged bash scripts/build/build-images.sh --verify --jobs 1 \
  --registry 127.0.0.1:5001 --tag online --services model3d-workflow-engine
infra_owned=1
run_logged bash scripts/infra/up.sh
run_logged python3 business/ftp/admission.py start
run_logged python3 -B -m scripts.test.online-file-model-nfs-fixture start
run_logged python3 -B -m scripts.test.online-file-model-minio-fixture start
ADDP_NFS_TEST_EXPORT="$(cat "$ADDP_ONLINE_SECRET_DIR/ftp-root")/data"
export ADDP_NFS_TEST_EXPORT
run_logged make test-common-nfs-integration
application_owned=1
for start_target in -security -meta -manager; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
start_online_model3d_runtime addp-ftp-model3d-runtime 127.0.0.1:5001/addp-model3d-workflow-engine:online
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite ftp-consumer-flow --wait-protection-owners manager --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/ftp-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_FTP_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/ftp-nfs-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_NFS_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py --descriptor "$ADDP_ONLINE_SECRET_DIR/ftp-minio-engine.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_MINIO_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
