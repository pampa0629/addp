#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=neo4j
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.neo4j-t2.yml
# ADDP_T2_INPUT_FILES=common/engine/propertygraph/ common/engine/plugin/property_graph.go graph/backend/ common/engine/plugins/neo4j/ common/dataprotection/projectionstore/ develop/backend/internal/protection/ develop/backend/internal/service/dev_executor.go
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
if compose up -d --wait --wait-timeout 240; then
    :
else
    startup_status=$?
    # Inspect only this Compose service before cleanup removes the evidence.
    # Emit no container environment; redact the generated fixture credential.
    {
        compose logs --no-color --tail 200 neo4j || true
        container_id=$(compose ps -a -q neo4j) || container_id=""
        if [ -n "$container_id" ]; then
            docker inspect --format '{"status":{{json .State.Status}},"exit_code":{{.State.ExitCode}},"oom_killed":{{.State.OOMKilled}},"error":{{json .State.Error}}}' "$container_id" || true
        fi
    } 2>&1 | python3 -c 'import os,sys; value=sys.stdin.read(); secret=os.environ.get("NEO4J_T2_PASSWORD", ""); sys.stdout.write(value.replace(secret, "[redacted]") if secret else value)' >&2 || true
    exit "$startup_status"
fi
export ADDP_NEO4J_INTEGRATION=1
export NEO4J_T2_ENDPOINT="$(compose port neo4j 7687)"
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/neo4j -run '^TestIntegrationNeo4j' -count=1 -v) 2>&1 | tee "$WORK_DIR/tests.log"
(cd "$ROOT_DIR/graph/backend" && GOWORK=off go test ./internal/service -run '^TestIntegrationNeo4jGraphDomain$' -count=1 -v) 2>&1 | tee -a "$WORK_DIR/tests.log"
grep -q -- '--- PASS: TestIntegrationNeo4jGraphDomain ' "$WORK_DIR/tests.log" || { echo 'Neo4j gate requires Graph domain contract test' >&2; exit 1; }
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then echo 'Neo4j T2 refuses skipped tests' >&2; exit 1; fi
grep -q -- '--- PASS: TestIntegrationNeo4j' "$WORK_DIR/tests.log"
echo 'NEO4J_T2_PASS local read scope, prepared graph, bounded results and rejection boundaries'
