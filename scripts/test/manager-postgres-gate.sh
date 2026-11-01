#!/usr/bin/env bash
# ADDP_T2_SERVICES=meilisearch
# ADDP_T2_OWNED_SERVICES=postgres
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.manager-postgres-t2.yml
# ADDP_T2_INPUT_FILES=scripts/infra/Dockerfile.postgres scripts/test/manager-postgres-gate_test.py scripts/test/native-effect-proxy.py
# ADDP_T2_REQUIRED_ENV=MANAGER_POSTGRES_TEST_DSN
# manager-postgres-gate.sh - Verify Manager lifecycle, geometry, preview and independent search snapshots.

set -euo pipefail

source_root=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --source-root)
            [ -n "${2:-}" ] || { echo '--source-root requires a checkout path' >&2; exit 2; }
            source_root="$2"; shift 2 ;;
        *) echo 'usage: manager-postgres-gate.sh [--source-root checkout]' >&2; exit 2 ;;
    esac
done

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SOURCE_ROOT="$ROOT_DIR"
if [ -n "$source_root" ]; then
    SOURCE_ROOT=$(cd "$source_root" && pwd -P) || exit 1
    source_checkout=$(git -C "$SOURCE_ROOT" rev-parse --show-toplevel 2>/dev/null) || { echo '--source-root must be an ADDP checkout' >&2; exit 1; }
    source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD 2>/dev/null) || { echo '--source-root must have a source commit' >&2; exit 1; }
    if [ "$source_checkout" != "$SOURCE_ROOT" ] ||
       ! git -C "$ROOT_DIR" merge-base --is-ancestor "$source_commit" HEAD ||
       ! [ -f "$SOURCE_ROOT/manager/backend/go.mod" ] ||
       ! grep -qx 'module github.com/addp/manager' "$SOURCE_ROOT/manager/backend/go.mod"; then
        echo '--source-root must be an ADDP checkout whose commit belongs to this repository' >&2
        exit 1
    fi
fi
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-manager-postgres.XXXXXX")
MS_CONTAINER=""
PG_OWNED=0
PG_IMAGE_ID=""
PG_EVIDENCE=""
compose() {
    docker compose --env-file /dev/null -p "$PG_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.manager-postgres-t2.yml" "$@"
}
cleanup() {
    local result=$? pg_cleanup=0 remaining current_id resource kind label_path
    trap - EXIT INT TERM
    set +e
    if [ "$PG_OWNED" = 1 ]; then
        local ownership=1
        for kind in container network volume; do
            label_path=.Labels
            if [ "$kind" = container ]; then
                remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$PG_PROJECT") || { ownership=0; pg_cleanup=1; }
                label_path=.Config.Labels
            else
                remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$PG_PROJECT") || { ownership=0; pg_cleanup=1; }
            fi
            for resource in $remaining; do
                if [ "$(docker "$kind" inspect -f "{{index $label_path \"addp.test-owner\"}}" "$resource")" != manager-postgres ] ||
                   [ "$(docker "$kind" inspect -f "{{index $label_path \"addp.test-run\"}}" "$resource")" != "$MANAGER_PG_TEST_OWNER" ]; then
                    ownership=0; pg_cleanup=1
                fi
            done
        done
        if [ "$ownership" = 1 ]; then
            compose down --volumes --remove-orphans --timeout 3 > "$WORK_DIR/pg-cleanup.log" 2>&1 || { cat "$WORK_DIR/pg-cleanup.log" >&2; pg_cleanup=1; }
        fi
        for resource in container network volume; do
            if [ "$resource" = container ]; then
                remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$PG_PROJECT") || pg_cleanup=1
            else
                remaining=$(docker "$resource" ls -q --filter "label=com.docker.compose.project=$PG_PROJECT") || pg_cleanup=1
            fi
            [ -z "$remaining" ] || pg_cleanup=1
        done
        if docker image inspect "$MANAGER_PG_TEST_IMAGE" >/dev/null 2>&1; then
            current_id=$(docker image inspect -f '{{.Id}}' "$MANAGER_PG_TEST_IMAGE")
            if [ -n "$PG_IMAGE_ID" ] && [ "$current_id" != "$PG_IMAGE_ID" ]; then
                pg_cleanup=1
            else
                docker image rm --no-prune "$MANAGER_PG_TEST_IMAGE" >/dev/null 2>&1 || pg_cleanup=1
            fi
        fi
        if docker image inspect "$MANAGER_PG_TEST_IMAGE" >/dev/null 2>&1; then pg_cleanup=1; fi
        if [ -n "$PG_EVIDENCE" ]; then
            python3 - "$PG_EVIDENCE" "$result" "$pg_cleanup" <<'PROOF'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]); value=json.loads(p.read_text()) if p.exists() else {}
value.update(test_exit_code=int(sys.argv[2]), cleanup='zero_residuals' if sys.argv[3]=='0' else 'failed')
p.write_text(json.dumps(value,indent=2)+'\n')
PROOF
            [ "$?" = 0 ] || result=1
        fi
        [ "$pg_cleanup" = 0 ] || result=1
        [ "$pg_cleanup" -ne 0 ] || echo 'Manager owned PostgreSQL containers, networks, volumes and image: zero residue'
    fi
    if [ -n "$MS_CONTAINER" ]; then
        if [ "$(docker inspect -f '{{index .Config.Labels "addp.test-owner"}}' "$MS_CONTAINER")" = manager-postgres ]; then
            docker rm -fv "$MS_CONTAINER" >/dev/null || result=1
        else result=1; fi
        if docker inspect "$MS_CONTAINER" >/dev/null 2>&1; then result=1; fi
    fi
    rm -rf "$WORK_DIR"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "${MANAGER_POSTGRES_CI_OWNED:-}" = 1 ]; then
    [ "${CI:-}" = true ] && [ "${GITHUB_ACTIONS:-}" = true ] &&
    [ "${GITHUB_JOB:-}" = manager-postgres ] && [ "${RUNNER_OS:-}" = Linux ] &&
    [ "${RUNNER_ARCH:-}" = X64 ] && [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] &&
    [ "${GITHUB_WORKSPACE:-}" = "$ROOT_DIR" ] || { echo 'Owned Manager PostgreSQL is restricted to its Linux GitHub Job' >&2; exit 1; }
    [[ "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ ]] && [[ "${GITHUB_RUN_ATTEMPT:-}" =~ ^[0-9]+$ ]] || exit 1
    [ -n "${RUNNER_TEMP:-}" ] && [ "${RUNNER_TEMP:0:1}" = / ] || exit 1
    [ "${MANAGER_POSTGRES_TEST_DSN:-}" = 'postgres://addp_ci:addp_ci_password@127.0.0.1:5432/addp_manager_disposable?sslmode=disable' ] || {
        echo 'Owned Manager PostgreSQL requires its exact disposable DSN' >&2; exit 1;
    }
    suffix=$(python3 -c 'import uuid; print(uuid.uuid4().hex[:12])')
    export MANAGER_PG_TEST_OWNER="$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$suffix"
    PG_PROJECT="addp-manager-pg-t2-$MANAGER_PG_TEST_OWNER"
    export MANAGER_PG_TEST_IMAGE="addp-manager-pg-t2:$MANAGER_PG_TEST_OWNER"
    if docker image inspect "$MANAGER_PG_TEST_IMAGE" >/dev/null 2>&1; then echo 'Refusing existing Manager test image' >&2; exit 1; fi
    mkdir -p "$RUNNER_TEMP/addp-manager-pg-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"
    PG_EVIDENCE="$RUNNER_TEMP/addp-manager-pg-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT/postgres-evidence.json"
    python3 - "$PG_EVIDENCE" "$(git -C "$ROOT_DIR" rev-parse HEAD)" <<'PROOF'
import json,sys
from pathlib import Path
Path(sys.argv[1]).write_text(json.dumps(dict(source=sys.argv[2],database='addp_manager_disposable',cleanup='pending'))+'\n')
PROOF
    PG_OWNED=1
    compose build postgres
    PG_IMAGE_ID=$(docker image inspect -f '{{.Id}}' "$MANAGER_PG_TEST_IMAGE")
    [[ "$PG_IMAGE_ID" =~ ^sha256:[a-f0-9]{64}$ ]] || exit 1
    [ "$(docker image inspect -f '{{index .Config.Labels "pgvector.source_revision"}}' "$MANAGER_PG_TEST_IMAGE")" = 2627c5ff775ae6d7aef0c430121ccf857842d2f2 ] || exit 1
    python3 - "$PG_EVIDENCE" "$PG_IMAGE_ID" <<'PROOF'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]); v=json.loads(p.read_text())
v.update(image_id=sys.argv[2],vector_source_revision='2627c5ff775ae6d7aef0c430121ccf857842d2f2')
p.write_text(json.dumps(v,indent=2)+'\n')
PROOF
    compose up -d --wait --wait-timeout 120 postgres
    container_id=$(compose ps -q postgres)
    [[ "$container_id" =~ ^[a-f0-9]{64}$ ]] || exit 1
    [ "$(docker inspect -f '{{.Image}}' "$container_id")" = "$PG_IMAGE_ID" ] || exit 1
    compose exec -T postgres psql -X -U addp_ci -d addp_manager_disposable -v ON_ERROR_STOP=1 -At > "$WORK_DIR/pg-capabilities.log" <<'SQL'
CREATE EXTENSION IF NOT EXISTS postgis;
BEGIN;
CREATE SCHEMA manager_extension_probe;
CREATE EXTENSION vector SCHEMA manager_extension_probe;
SELECT json_build_object('postgres_version', version(), 'server_version_num', current_setting('server_version_num'), 'postgis_version', postgis_lib_version(), 'vector_version', (SELECT extversion FROM pg_extension WHERE extname='vector'), 'vector_probe', '[1,2]'::manager_extension_probe.vector::text);
ROLLBACK;
SELECT count(*) FROM pg_namespace WHERE nspname='manager_extension_probe';
SQL
    python3 - "$WORK_DIR/pg-capabilities.log" "$PG_EVIDENCE" "$PG_IMAGE_ID" "$(git -C "$ROOT_DIR" rev-parse HEAD)" <<'PROOF'
import json,sys
from pathlib import Path
lines=Path(sys.argv[1]).read_text().splitlines()
rows=[json.loads(line) for line in lines if line.startswith('{')]
assert len(rows)==1 and lines[-1]=='0', 'extension probe did not roll back'
v=rows[0]
v.update(image_id=sys.argv[3],source=sys.argv[4],vector_source_revision='2627c5ff775ae6d7aef0c430121ccf857842d2f2',database='addp_manager_disposable',cleanup='pending')
Path(sys.argv[2]).write_text(json.dumps(v,indent=2)+'\n');print(json.dumps(v))
assert 150000<=int(v['server_version_num'])<160000 and v['postgis_version'].startswith('3.') and v['vector_version']=='0.8.0' and v['vector_probe']=='[1,2]', 'required real extensions unavailable'
PROOF
elif [ "${CI:-}" = true ]; then
    echo 'Manager CI requires its repository-built owned PostgreSQL' >&2
    exit 1
fi

if [ -z "${MANAGER_POSTGRES_TEST_DSN:-}" ]; then
    echo "MANAGER_POSTGRES_TEST_DSN must reference addp_test or a disposable PostgreSQL database" >&2
    exit 1
fi
case "$MANAGER_POSTGRES_TEST_DSN" in postgres://*/*|postgresql://*/*) ;; *) echo "MANAGER_POSTGRES_TEST_DSN must use a PostgreSQL URL" >&2; exit 1 ;; esac
dsn_without_query=${MANAGER_POSTGRES_TEST_DSN%%\?*}
database=${dsn_without_query##*/}
case "$database" in addp_test) ;; addp_manager_disposable) [ "$PG_OWNED" = 1 ] || exit 1 ;; *) echo "MANAGER_POSTGRES_TEST_DSN must identify addp_test or a disposable database" >&2; exit 1 ;; esac

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

cd "$SOURCE_ROOT/manager/backend"
# These packages initialize the same Manager schema; serialize their DDL and fixtures.
ADDP_POSTGRES_INTEGRATION=1 \
    go test -p 1 ./internal/repository ./internal/service ./internal/preview \
    -run '^TestIntegrationPostgresManager' \
    -count=1 -v 2>&1 | tee "$WORK_DIR/manager-postgres.log"

ADDP_POSTGRES_INTEGRATION=1 \
    go test ./internal/api -run '^TestIntegrationPostgresManagerExecutionReadIsolation$' \
    -count=1 -v 2>&1 | tee -a "$WORK_DIR/manager-postgres.log"

if grep -q -- '--- SKIP:' "$WORK_DIR/manager-postgres.log"; then
    echo "Manager PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi

for required in TestIntegrationPostgresManagerCleanupProductionRepositories TestIntegrationPostgresManagerCleanupRuntimeAtomicity TestIntegrationPostgresManagerContentSnapshots TestIntegrationPostgresManagerExecutionReadIsolation; do
    grep -q -- "^--- PASS: $required " "$WORK_DIR/manager-postgres.log" || {
        echo "Manager PostgreSQL gate requires the real $required test" >&2; exit 1;
    }
done
echo 'Manager PostgreSQL full lifecycle, production initialization and native index contracts passed'
