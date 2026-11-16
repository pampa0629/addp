#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=rustfs
# ADDP_T2_COMPOSE_FILE=business/docker-compose.yml
# ADDP_T2_INPUT_FILES=business/docker-compose.yml business/scripts/rustfs.sh business/.env.example common/engine/plugins/rustfs/ common/engine/plugins/objectstore/ common/engine/plugins/s3/ common/engine/plugins/builtin/ common/dataitem/authorizationfacts/ common-frontend/basic/src/utils/engineApprovalInitialization.mjs meta/backend/internal/scanruntime/ meta/backend/internal/service/ manager/backend/internal/preview/ manager/backend/internal/api/ manager/backend/internal/service/storage_model_read.go system/backend/internal/service/ system/backend/internal/engineaccess/ system/backend/internal/migration/
# disposable RustFS: reuse Business image/config, own every resource and remove it.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
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
docker compose --env-file /dev/null --file "$ROOT_DIR/business/docker-compose.yml" --profile rustfs config --format json > "$WORK_DIR/business.json"
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
run_contract() {
    export ADDP_RUSTFS_TEST_ENDPOINT="$(compose port rustfs 9000)"
    (cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/rustfs -run '^TestIntegrationRustFS' -count=1 -timeout=90s -v) | tee "$WORK_DIR/provider.log"
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
    (cd "$ROOT_DIR/$owner/backend" && GOWORK=off go test "./internal/$package" -run '^TestIntegrationRustFS' -count=1 -timeout=90s -v) | tee "$WORK_DIR/$owner.log"
    ! grep -q -- '--- SKIP:' "$WORK_DIR/$owner.log"
    grep -q -- "--- PASS: TestIntegrationRustFS$contract " "$WORK_DIR/$owner.log"
done
echo 'RUSTFS_T2_PASS native provider, persistence and consumers'
