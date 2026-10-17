#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=falkordb
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.falkordb-engine-t2.yml
# ADDP_T2_INPUT_FILES=common/engine/propertygraph/ common/engine/plugin/property_graph.go graph/backend/ common/engine/plugins/falkordb/ common/query/cypherread/ common/dataprotection/projectionstore/ develop/backend/internal/protection/ develop/backend/internal/service/ meta/backend/internal/scanruntime/ scripts/infra/falkordb.yml
# Own a disposable graph service; never connect to Ontology Infra or business graphs.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-falkordb-t2.XXXXXX")
COMPOSE_PROJECT="addp-falkordb-t2-${PPID}-$$"
export INFRA_FALKORDB_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
compose() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.falkordb-engine-t2.yml" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || status=1
    if docker ps -a --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q . || docker volume ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.Name}}' | grep -q . || docker network ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q .; then
        echo 'FalkorDB T2 cleanup left resources' >&2; status=1
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --wait --wait-timeout 120
export ADDP_FALKORDB_INTEGRATION=1
export FALKORDB_T2_ENDPOINT="$(compose port falkordb 6379)"
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/falkordb -run '^TestIntegrationFalkorDB' -count=1 -v) 2>&1 | tee "$WORK_DIR/tests.log"
(cd "$ROOT_DIR/meta/backend" && GOWORK=off go test ./internal/scanruntime -run '^TestIntegrationFalkorDB' -count=1 -v) 2>&1 | tee -a "$WORK_DIR/tests.log"
(cd "$ROOT_DIR/develop/backend" && GOWORK=off go test ./internal/service -run '^TestIntegrationFalkorDB' -count=1 -v) 2>&1 | tee -a "$WORK_DIR/tests.log"
(cd "$ROOT_DIR/graph/backend" && GOWORK=off go test ./internal/service -run '^TestIntegrationFalkorDBGraphDomain$' -count=1 -v) 2>&1 | tee -a "$WORK_DIR/tests.log"
grep -q -- '--- PASS: TestIntegrationFalkorDBGraphDomain ' "$WORK_DIR/tests.log" || { echo 'FalkorDB gate requires Graph domain contract test' >&2; exit 1; }
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then echo 'FalkorDB T2 refuses skipped tests' >&2; exit 1; fi
grep -q -- '--- PASS: TestIntegrationFalkorDB' "$WORK_DIR/tests.log"
echo 'FALKORDB_T2_PASS catalog, parameters, prepared reads, graph results and protection boundaries'
