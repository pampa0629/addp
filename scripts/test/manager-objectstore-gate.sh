#!/usr/bin/env bash
# Nested lifecycle owned by make test-manager-postgres.
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_ROOT=${1:?source checkout required}
run=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
export MANAGER_PG_TEST_OWNER="$run"
export MANAGER_PG_TEST_IMAGE="addp-manager-object-unused:$run"
export MANAGER_OBJECT_TEST_IMAGE="addp-manager-objects:$run"
export MANAGER_OBJECT_TEST_USER="objects_$run"
export MANAGER_OBJECT_TEST_PASSWORD
MANAGER_OBJECT_TEST_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(24))')
project="addp-manager-objects-$run"
image_id=""
log=""

compose() {
    docker compose --env-file /dev/null -p "$project" -f "$ROOT_DIR/scripts/test/docker-compose.manager-postgres-t2.yml" "$@"
}

cleanup() {
    local status=$? ownership=1 kind labels remaining resource current_id
    trap - EXIT INT TERM
    set +e
    for kind in container network volume; do
        labels=.Labels
        if [ "$kind" = container ]; then
            remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$project") || { ownership=0; status=1; }
            labels=.Config.Labels
        else
            remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project") || { ownership=0; status=1; }
        fi
        for resource in $remaining; do
            if [ "$(docker "$kind" inspect -f "{{index $labels \"addp.test-owner\"}}" "$resource")" != manager-postgres ] ||
               [ "$(docker "$kind" inspect -f "{{index $labels \"addp.test-run\"}}" "$resource")" != "$run" ]; then
                ownership=0
            fi
        done
    done
    if [ "$ownership" = 1 ]; then
        compose down --volumes --remove-orphans --timeout 3 >/dev/null 2>&1 || status=1
    else
        status=1
    fi
    for kind in container network volume; do
        if [ "$kind" = container ]; then
            remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$project") || status=1
        else
            remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project") || status=1
        fi
        [ -z "$remaining" ] || status=1
    done
    if docker image inspect "$MANAGER_OBJECT_TEST_IMAGE" >/dev/null 2>&1; then
        current_id=$(docker image inspect -f '{{.Id}}' "$MANAGER_OBJECT_TEST_IMAGE")
        if [ -z "$image_id" ] || [ "$image_id" = "$current_id" ]; then
            docker image rm --no-prune "$MANAGER_OBJECT_TEST_IMAGE" >/dev/null 2>&1 || status=1
        else
            status=1
        fi
    fi
    if docker image inspect "$MANAGER_OBJECT_TEST_IMAGE" >/dev/null 2>&1; then status=1; fi
    if [ -n "$log" ]; then rm -f "$log"; fi
    [ "$status" != 0 ] || echo 'Manager owned MinIO containers, networks, volumes and image: zero residue'
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

compose build minio
image_id=$(docker image inspect -f '{{.Id}}' "$MANAGER_OBJECT_TEST_IMAGE")
compose up -d --wait --wait-timeout 90 minio
export MANAGER_OBJECT_TEST_ENDPOINT
MANAGER_OBJECT_TEST_ENDPOINT=$(compose port minio 9000)
[[ $MANAGER_OBJECT_TEST_ENDPOINT == 127.0.0.1:* ]]
export MINIO_ENDPOINT="$MANAGER_OBJECT_TEST_ENDPOINT" MINIO_ROOT_USER="$MANAGER_OBJECT_TEST_USER" MINIO_ROOT_PASSWORD="$MANAGER_OBJECT_TEST_PASSWORD" MINIO_USE_SSL=false
export ADDP_INFRA_OBJECTSTORE_PROVIDER=minio ADDP_INFRA_OBJECTSTORE_BINDINGS= ADDP_MANAGER_OBJECT_INTEGRATION=1
log=$(mktemp "${TMPDIR:-/tmp}/addp-manager-objects.XXXXXX")
(cd "$SOURCE_ROOT/common" && go test ./infra/objectstore/... ./exportartifact -run 'TestManagerObjectAreaAgainstMinIO|TestExportAreaAgainstMinIO' -count=1 -v) 2>&1 | tee "$log"
if grep -q -- '--- SKIP:' "$log"; then
    echo 'Manager object gate refuses skipped tests' >&2
    exit 1
fi
for required in TestManagerObjectAreaAgainstMinIO TestExportAreaAgainstMinIO; do
    grep -q -- "--- PASS: $required " "$log" || { echo "missing required test: $required" >&2; exit 1; }
done
