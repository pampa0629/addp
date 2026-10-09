#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres,meilisearch
# ADDP_T2_REQUIRED_ENV=MANAGER_POSTGRES_TEST_DSN
# manager-postgres-gate.sh - Verify Manager lifecycle, geometry, preview and independent search snapshots.

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-manager-postgres.XXXXXX")
MS_CONTAINER=""
cleanup() {
    local result=$?
    if [ -n "$MS_CONTAINER" ]; then
        docker rm -fv "$MS_CONTAINER" >/dev/null || result=1
        if docker inspect "$MS_CONTAINER" >/dev/null 2>&1; then result=1; fi
    fi
    rm -rf "$WORK_DIR"
    exit "$result"
}
trap cleanup EXIT

if [ -z "${MANAGER_POSTGRES_TEST_DSN:-}" ]; then
    echo "MANAGER_POSTGRES_TEST_DSN must reference addp_test or a disposable PostgreSQL database" >&2
    exit 1
fi
case "$MANAGER_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "MANAGER_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
dsn_without_query=${MANAGER_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in addp_test|*disposable*) ;; *) echo "MANAGER_POSTGRES_TEST_DSN must identify addp_test or a disposable database" >&2; exit 1 ;; esac

source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$MANAGER_POSTGRES_TEST_DSN"

# Use an exclusive, ephemeral search instance; never connect to developer content.
if [ "${CI:-}" = "true" ]; then
    : "${MANAGER_MEILISEARCH_TEST_URL:?CI must supply its owned Meilisearch service}"
    : "${MANAGER_MEILISEARCH_TEST_KEY:?CI must supply its test key}"
else
    if [ -n "${MANAGER_MEILISEARCH_TEST_URL:-}" ]; then
        echo "Local gate owns its Meilisearch fixture; external endpoints are refused" >&2
        exit 1
    fi
    ms_name="addp-manager-snapshots-$(basename "$WORK_DIR" | tr '[:upper:]' '[:lower:]')"
    MANAGER_MEILISEARCH_TEST_KEY=$(python3 -c 'import secrets; print(secrets.token_hex(24))')
    export MANAGER_MEILISEARCH_TEST_KEY
    ms_image=$(python3 - "$ROOT_DIR/docker-compose.infra.yml" <<'IMAGE'
import pathlib, re, sys
text = pathlib.Path(sys.argv[1]).read_text()
match = re.search(r'image: (getmeili/meilisearch:[^\s]+@sha256:[0-9a-f]{64})', text)
if not match: raise SystemExit('Pinned Meilisearch image required')
print(match.group(1))
IMAGE
)
    MS_CONTAINER=$(docker run -d --name "$ms_name" --label addp.test-owner=manager-postgres \
        --tmpfs /meili_data -p 127.0.0.1::7700 -e MEILI_MASTER_KEY="$MANAGER_MEILISEARCH_TEST_KEY" \
        -e MEILI_NO_ANALYTICS=true "$ms_image")
    ms_port=$(docker port "$MS_CONTAINER" 7700/tcp | sed 's/.*://')
    export MANAGER_MEILISEARCH_TEST_URL="http://127.0.0.1:$ms_port"
fi
python3 - <<'HEALTH'
import os, time, urllib.request
url = os.environ['MANAGER_MEILISEARCH_TEST_URL'] + '/health'
for attempt in range(120):
    try:
        with urllib.request.urlopen(url, timeout=1) as response:
            if response.status == 200: break
    except Exception: pass
    time.sleep(.25)
else: raise SystemExit('Meilisearch fixture did not become healthy')
HEALTH

cd "$ROOT_DIR/manager/backend"
ADDP_POSTGRES_INTEGRATION=1 \
    go test ./internal/repository ./internal/service ./internal/preview \
    -run '^TestIntegrationPostgresManager' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/manager-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/manager-postgres.log"; then
    echo "Manager PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
