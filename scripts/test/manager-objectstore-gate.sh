#!/usr/bin/env bash
# Shared disposable MinIO lifecycle for Manager and Develop standard gates.
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
    if [ -n "${notebook_venv:-}" ]; then rm -rf "$notebook_venv"; fi
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
object_packages=(./infra/objectstore/... ./exportartifact)
object_tests='TestManagerObjectAreaAgainstMinIO|TestExportAreaAgainstMinIO'
required_tests=(TestManagerObjectAreaAgainstMinIO TestExportAreaAgainstMinIO)
if [ "${2:-}" = --notebook ]; then
    object_packages=(./infra/objectstore/...)
    object_tests='^TestManagerObjectAreaAgainstMinIO$'
    required_tests=(TestManagerObjectAreaAgainstMinIO)
fi
(cd "$SOURCE_ROOT/common" && go test "${object_packages[@]}" -run "$object_tests" -count=1 -v) 2>&1 | tee "$log"
if grep -q -- '--- SKIP:' "$log"; then
    echo 'Manager object gate refuses skipped tests' >&2
    exit 1
fi
for required in "${required_tests[@]}"; do
    grep -q -- "--- PASS: $required " "$log" || { echo "missing required test: $required" >&2; exit 1; }
done

if [ "${2:-}" = --notebook ]; then
    COMPOSE_PROJECT_NAME="$project" ADDP_INFRA_RESOLVED=1 \
      ADDP_INFRA_OBJECTSTORE_BINDINGS='standard=standard-exception,develop=notebook-exception' \
      bash "$SOURCE_ROOT/scripts/infra/init-minio.sh" --compose-file "$ROOT_DIR/scripts/test/docker-compose.manager-postgres-t2.yml"
    export NOTEBOOK_READONLY_USER="notebook_reader"
    export NOTEBOOK_READONLY_PASSWORD
    NOTEBOOK_READONLY_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_hex(24))')
    compose exec -T -e NOTEBOOK_READONLY_USER -e NOTEBOOK_READONLY_PASSWORD minio sh <<'READONLY'
set -e
mc alias set notebook http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
mc admin user add notebook "$NOTEBOOK_READONLY_USER" "$NOTEBOOK_READONLY_PASSWORD" >/dev/null
echo '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetBucketLocation","s3:ListBucket","s3:GetObject"],"Resource":["arn:aws:s3:::notebook-exception","arn:aws:s3:::notebook-exception/*"]}]}' | mc admin policy create notebook notebook-readonly /dev/stdin >/dev/null
mc admin policy attach notebook notebook-readonly --user "$NOTEBOOK_READONLY_USER" >/dev/null
READONLY
    notebook_base_python=python3
    if [ -x "$SOURCE_ROOT/engines/jupyter/venv/bin/python" ]; then
        notebook_base_python="$SOURCE_ROOT/engines/jupyter/venv/bin/python"
    fi
    notebook_venv=$(mktemp -d "${TMPDIR:-/tmp}/addp-notebook-venv.XXXXXX")
    "$notebook_base_python" -m venv "$notebook_venv"
    export NOTEBOOK_STORAGE_PYTHON="$notebook_venv/bin/python"
    "$NOTEBOOK_STORAGE_PYTHON" -m pip install -e "$SOURCE_ROOT/common-python[infra-minio]"
    export PYTHONPATH="$SOURCE_ROOT/common-python${PYTHONPATH:+:$PYTHONPATH}"
    export NOTEBOOK_STORAGE_PROBE="$SOURCE_ROOT/common-python/tests/notebook_storage_probe.py"
    export ADDP_NOTEBOOK_OBJECT_INTEGRATION=1
    (cd "$SOURCE_ROOT/develop/backend" && go test ./internal/service -run '^TestNotebookObjectsAcrossLanguagesAgainstMinIO$' -count=1 -v) 2>&1 | tee "$log"
    ! grep -q -- '--- SKIP:' "$log"
    grep -q -- '--- PASS: TestNotebookObjectsAcrossLanguagesAgainstMinIO ' "$log"
fi
