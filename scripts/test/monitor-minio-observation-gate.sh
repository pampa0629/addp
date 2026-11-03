#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=minio-infra,minio-business,promtool
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.minio-observation-t2.yml
# ADDP_T2_INPUT_FILES=scripts/test/metrics-source-recovery_test.py scripts/infra/metrics-sources.py scripts/test/metrics-sources_test.py monitor/backend/internal/resourcequery/ monitor/backend/internal/miniosource/ monitor/backend/internal/sourceserver/ monitor/backend/cmd/minio-metrics-source/ monitor/backend/go.mod monitor/backend/go.sum common/go.mod common/go.sum common/jsonmap/ scripts/infra/Dockerfile.minio-metrics scripts/utils/metrics-source.py scripts/test/metrics-source-fixture.py scripts/infra/minio-observation.py scripts/infra/minio-observation/ scripts/infra/Dockerfile.minio scripts/utils/private-state.py scripts/utils/deployment-docker.py scripts/utils/deployment-inventory.py scripts/test/minio-observation-lifecycle_test.py scripts/test/minio-observation-probe.py scripts/test/docker-compose.minio-observation-t2.yml scripts/infra/up.sh scripts/infra/down.sh business/scripts/start.sh business/scripts/stop.sh .env.example business/.env.example
# Owns disposable containers, volumes, private credentials and cleanup.
set -euo pipefail
[[ $# == 0 ]] || { echo 'MinIO observation T2: unexpected arguments' >&2; exit 2; }
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"
make test-monitor-minio-observation-unit
export T2_MINIO_PROJECT="addp-minio-observation-t2-$(python3 -c 'import secrets;print(secrets.token_hex(6))')"
export ADDP_MINIO_OBSERVATION_T2=1
export T2_MINIO_TEST_IMAGE="$T2_MINIO_PROJECT:local"
export T2_MINIO_STATE
T2_MINIO_STATE=$(mktemp -d "${TMPDIR:-/tmp}/addp-minio-observation-t2.XXXXXX")
T2_MINIO_STATE=$(cd "$T2_MINIO_STATE" && pwd -P)
chmod 700 "$T2_MINIO_STATE"
export T2_MINIO_INFRA_USER="infra-$(python3 -c 'import secrets;print(secrets.token_hex(8))')"
export T2_MINIO_BUSINESS_USER="business-$(python3 -c 'import secrets;print(secrets.token_hex(8))')"
export T2_MINIO_INFRA_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(32))')"
export T2_MINIO_BUSINESS_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(32))')"
compose=(docker compose --project-name "$T2_MINIO_PROJECT" --project-directory "$ROOT_DIR/scripts/test" -f "$ROOT_DIR/scripts/test/docker-compose.minio-observation-t2.yml")
compose() { "${compose[@]}" "$@"; }
cleanup() {
  result=$?
  trap - EXIT
  set +e
  python3 scripts/test/minio-observation-probe.py --cleanup || result=1
  "${compose[@]}" --profile minio-observation down --volumes --remove-orphans >/dev/null 2>&1 || result=1
  for kind in container network volume; do
    if [[ "$kind" == container ]]; then
      remaining=$(docker container ls -a -q --filter "label=com.docker.compose.project=$T2_MINIO_PROJECT") || result=1
    else
      remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$T2_MINIO_PROJECT") || result=1
    fi
    [[ -z "$remaining" ]] || { echo "MinIO observation T2 cleanup: $kind remains" >&2; result=1; }
  done
  if docker image inspect "$T2_MINIO_TEST_IMAGE" >/dev/null 2>&1; then
    docker image rm "$T2_MINIO_TEST_IMAGE" >/dev/null || result=1
  fi
  rm -rf "$T2_MINIO_STATE"
  [[ ! -e "$T2_MINIO_STATE" ]] || result=1
  [[ $result == 0 ]] && echo 'MinIO observation T2: PASS; containers/networks/volumes/private state = 0'
  exit "$result"
}
trap cleanup EXIT
compose build minio-infra
"${compose[@]}" up -d --no-build --wait --wait-timeout 90
python3 scripts/test/minio-observation-probe.py

(cd monitor/backend && GOWORK=off go test ./internal/resourcequery -run '^TestIntegrationMinIOMetricQueries$' -count=1 -v)
