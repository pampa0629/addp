#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_TEST_POSTGRES_PORT,ADDP_TEST_POSTGRES_PASSWORD
# ADDP_T2_OWNED_SERVICES=rustfs
# ADDP_T2_COMPOSE_FILE=business/docker-compose.yml
# ADDP_T2_INPUT_FILES=business/docker-compose.yml business/scripts/rustfs.sh business/.env.example common/execution/ common/client/ transfer/backend/internal/planner/ transfer/backend/internal/executor/ transfer/backend/internal/service/ transfer/backend/internal/repository/ common/infra/objectstore/ standard/backend/internal/service/document_service_integration_test.go common-python/ engines/jupyter/interactive_sessions.py common/engine/plugins/rustfs/ common/engine/plugins/objectstore/ common/engine/plugins/s3/ common/engine/plugins/builtin/ common/dataitem/authorizationfacts/ common-frontend/basic/src/utils/engineApprovalInitialization.mjs meta/backend/internal/scanruntime/ meta/backend/internal/service/ manager/backend/internal/preview/ manager/backend/internal/api/ manager/backend/internal/service/storage_model_read.go system/backend/internal/service/ system/backend/internal/engineaccess/ system/backend/internal/migration/
# disposable RustFS: reuse Business image/config, own every resource and remove it.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
SOURCE_ROOT=$ROOT_DIR
test_group=all
if [ "$#" -eq 2 ] && [ "$1" = --test ]; then
    case "$2" in
        platform-object-area|platform-standard|platform-notebook) test_group=$2 ;;
        *) echo "unknown RustFS test group" >&2; exit 2 ;;
    esac
    shift 2
fi
if [ "$#" -ne 0 ]; then
    if [ "$#" -ne 2 ] || [ "$1" != --source-root ]; then
        echo "usage: $0 [--source-root checkout|--test platform-object-area|platform-standard|platform-notebook]" >&2
        exit 2
    fi
    SOURCE_ROOT=$(cd "$2" && pwd -P)
    if [ "$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel)" != "$SOURCE_ROOT" ] ||
       ! git -C "$ROOT_DIR" merge-base --is-ancestor "$(git -C "$SOURCE_ROOT" rev-parse HEAD)" HEAD ||
       ! grep -qx 'module github.com/addp/transfer' "$SOURCE_ROOT/transfer/backend/go.mod"; then
        echo "--source-root must be an ADDP checkout whose commit belongs to this repository" >&2
        exit 2
    fi
fi
if [ "$test_group" = all ] || [ "$test_group" = platform-standard ]; then
    : "${ADDP_TEST_POSTGRES_PORT:?verified PostgreSQL test mapping required}"
    : "${ADDP_TEST_POSTGRES_PASSWORD:?PostgreSQL test password required}"
    source "$ROOT_DIR/scripts/infra/ports.sh"
    addp_infra_verify_test_postgres_target "${ADDP_TEST_POSTGRES_HOST:-localhost}" "$ADDP_TEST_POSTGRES_PORT" "${ADDP_TEST_POSTGRES_DATABASE:-addp_test}"
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-rustfs-t2.XXXXXX")
chmod 700 "$WORK_DIR"
export RUSTFS_T2_PROJECT="addp-rustfs-t2-${PPID}-$$"
export RUSTFS_ACCESS_KEY="$(python3 -c 'import secrets; print(secrets.token_hex(10))')"
export RUSTFS_SECRET_KEY="$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')"
compose() { docker compose --project-name "$RUSTFS_T2_PROJECT" --file "$WORK_DIR/compose.json" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    if [ -f "$WORK_DIR/compose.json" ]; then
        compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    fi
    for kind in container volume network; do
        if [ -n "$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$RUSTFS_T2_PROJECT")" ]; then
            echo "RustFS T2 cleanup left $kind resources" >&2; status=1
        fi
    done
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
docker compose --env-file /dev/null --file "$SOURCE_ROOT/business/docker-compose.yml" --profile rustfs config --format json > "$WORK_DIR/business.json"
python3 - "$WORK_DIR" <<'PY'
import json, sys
from pathlib import Path
work = Path(sys.argv[1])
service = json.loads((work/'business.json').read_text())['services']['rustfs']
for key in ('container_name', 'profiles', 'networks'):
    service.pop(key, None)
service['restart'] = 'no'
for port in service['ports']:
    port['published'] = '0'
(work/'compose.json').write_text(json.dumps({'services': {'rustfs': service}, 'volumes': {'rustfs_data': {}}}))
PY
compose up -d --wait --wait-timeout 150
export ADDP_RUSTFS_INTEGRATION=1 ADDP_RUSTFS_TEST_BUCKET=addp-rustfs-contract
export ADDP_RUSTFS_TEST_ACCESS_KEY="$RUSTFS_ACCESS_KEY" ADDP_RUSTFS_TEST_SECRET_KEY="$RUSTFS_SECRET_KEY"
export ADDP_RUSTFS_TEST_ENDPOINT="$(compose port rustfs 9000)"
(cd "$SOURCE_ROOT/common" && GOWORK=off go test ./infra/objectstore/s3 -run '^TestPlatformAreaAgainstRustFS$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/platform-s3.log"
grep -q '^--- PASS: TestPlatformAreaAgainstRustFS ' "$WORK_DIR/platform-s3.log"
if [ "$test_group" = platform-object-area ]; then
    exit 0
fi

if [ "$test_group" = platform-notebook ]; then
    python3 -m venv "$WORK_DIR/notebook-python"
    export NOTEBOOK_STORAGE_PYTHON="$WORK_DIR/notebook-python/bin/python"
    "$NOTEBOOK_STORAGE_PYTHON" -m pip install "$SOURCE_ROOT/common-python[infra-minio]"
    export NOTEBOOK_STORAGE_PROBE="$SOURCE_ROOT/common-python/tests/notebook_storage_probe.py"
    export NOTEBOOK_STORAGE_TEST_PROVIDER=rustfs ADDP_NOTEBOOK_OBJECT_INTEGRATION=1
    export ADDP_INFRA_OBJECTSTORE_PROVIDER=minio MINIO_USE_SSL=false
    export MINIO_ENDPOINT="$ADDP_RUSTFS_TEST_ENDPOINT"
    export MINIO_ROOT_USER="$ADDP_RUSTFS_TEST_ACCESS_KEY" MINIO_ROOT_PASSWORD="$ADDP_RUSTFS_TEST_SECRET_KEY"
    (cd "$SOURCE_ROOT/common" && GOWORK=off go test ./infra/objectstore/bootstrap \
        -run '^TestNotebookObjectsAcrossLanguagesAgainstS3$' -count=1 -timeout=120s -v) | tee "$WORK_DIR/notebook.log"
    grep -q '^--- PASS: TestNotebookObjectsAcrossLanguagesAgainstS3 ' "$WORK_DIR/notebook.log"
    exit 0
fi

export STANDARD_POSTGRES_TEST_DSN
STANDARD_POSTGRES_TEST_DSN=$(python3 - <<'PYTHON'
import os
from urllib.parse import quote

env = os.environ
user = quote(env.get("ADDP_TEST_POSTGRES_USER", "addp"), safe="")
password = quote(env["ADDP_TEST_POSTGRES_PASSWORD"], safe="")
host = env.get("ADDP_TEST_POSTGRES_HOST", "localhost")
port = env["ADDP_TEST_POSTGRES_PORT"]
database = env.get("ADDP_TEST_POSTGRES_DATABASE", "addp_test")
print(f"postgres://{user}:{password}@{host}:{port}/{database}?sslmode=disable")
PYTHON
)
(cd "$SOURCE_ROOT/standard/backend" && ADDP_STANDARD_S3_TEST_PROVIDER=rustfs \
    STANDARD_S3_TEST_ENDPOINT="$ADDP_RUSTFS_TEST_ENDPOINT" \
    STANDARD_S3_TEST_ACCESS_KEY="$ADDP_RUSTFS_TEST_ACCESS_KEY" \
    STANDARD_S3_TEST_SECRET_KEY="$ADDP_RUSTFS_TEST_SECRET_KEY" \
    GOWORK=off go test ./internal/service -run '^TestDocumentFileLifecycleAgainstPostgresAndS3$' -count=1 -timeout=90s -v) \
    | tee "$WORK_DIR/standard.log"
grep -q '^--- PASS: TestDocumentFileLifecycleAgainstPostgresAndS3 ' "$WORK_DIR/standard.log"
if [ "$test_group" = platform-standard ]; then
    exit 0
fi

run_contract() {
    (cd "$SOURCE_ROOT/common" && GOWORK=off go test ./engine/plugins/rustfs -run '^TestIntegrationRustFS' -count=1 -timeout=90s -v) | tee "$WORK_DIR/provider.log"
    ! grep -q -- '--- SKIP:' "$WORK_DIR/provider.log"
    grep -q -- '--- PASS: TestIntegrationRustFSProvider' "$WORK_DIR/provider.log"
}
export ADDP_RUSTFS_TEST_PHASE=initial
run_contract
# Recreate the server with the same named volume and independently assigned ports.
compose rm -sf rustfs
compose up -d --wait --wait-timeout 150
export ADDP_RUSTFS_TEST_PHASE=persisted
run_contract
export ADDP_RUSTFS_TEST_META_ITEM="$WORK_DIR/meta-item.json"
for specification in 'system service SystemConnection' 'meta service DeepScan' 'manager preview Preview' 'manager api AuthorizedPreview'; do
    read -r owner package contract <<< "$specification"
    (cd "$SOURCE_ROOT/$owner/backend" && GOWORK=off go test "./internal/$package" -run '^TestIntegrationRustFS' -count=1 -timeout=90s -v) | tee "$WORK_DIR/$owner.log"
    ! grep -q -- '--- SKIP:' "$WORK_DIR/$owner.log"
    grep -q -- "--- PASS: TestIntegrationRustFS$contract " "$WORK_DIR/$owner.log"
done
(cd "$SOURCE_ROOT/transfer/backend" && GOWORK=off go test ./internal/planner -run '^TestIntegrationRustFSParquetImport$' -count=1 -timeout=90s -v) | tee "$WORK_DIR/transfer.log"
! grep -q -- '--- SKIP:' "$WORK_DIR/transfer.log"
grep -q -- '--- PASS: TestIntegrationRustFSParquetImport ' "$WORK_DIR/transfer.log"
echo 'RUSTFS_T2_PASS native provider, persistence, consumers and PostgreSQL import'
