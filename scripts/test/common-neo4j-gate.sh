#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=neo4j
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.neo4j-t2.yml
# ADDP_T2_INPUT_FILES=common/engine/plugins/neo4j/ common/dataprotection/projectionstore/ develop/backend/internal/protection/ develop/backend/internal/service/dev_executor.go
# Own a disposable deployment; never reuse a business engine.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-neo4j-t2.XXXXXX")
COMPOSE_PROJECT="addp-neo4j-t2-${PPID}-$$"
export NEO4J_T2_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
compose() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.neo4j-t2.yml" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || status=1
    if docker ps -a --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q . || docker volume ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.Name}}' | grep -q . || docker network ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q .; then
        echo 'Neo4j T2 cleanup left resources' >&2; status=1
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --wait --wait-timeout 240
export ADDP_NEO4J_INTEGRATION=1
export NEO4J_T2_ENDPOINT="$(compose port neo4j 7687)"
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/neo4j -run '^TestIntegrationNeo4j' -count=1 -v) 2>&1 | tee "$WORK_DIR/tests.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then echo 'Neo4j T2 refuses skipped tests' >&2; exit 1; fi
grep -q -- '--- PASS: TestIntegrationNeo4j' "$WORK_DIR/tests.log"
echo 'NEO4J_T2_PASS local read scope, prepared graph, bounded results and rejection boundaries'
