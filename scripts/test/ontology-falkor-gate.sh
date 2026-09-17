#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=falkordb
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.ontology-falkor-t2.yml
# Own the complete lifecycle of a disposable FalkorDB, never a developer endpoint.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-ontology-falkor.XXXXXX")
RUN_ID=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
COMPOSE_PROJECT="addp-ontology-falkor-t2-$RUN_ID"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.ontology-falkor-t2.yml"
export ONTOLOGY_FALKOR_TEST_PASSWORD="$RUN_ID"
compose() { docker compose --env-file /dev/null -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }
cleanup() {
    local result=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans > "$WORK_DIR/cleanup.log" 2>&1 || result=1
    local remaining
    remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || result=1
    if [ -n "$remaining" ]; then
        echo "Ontology FalkorDB cleanup left owned containers" >&2
        result=1
    fi
    remaining=$(docker network ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || result=1
    if [ -n "$remaining" ]; then result=1; fi
    remaining=$(docker volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT") || result=1
    if [ -n "$remaining" ]; then result=1; fi
    if [ "$result" -ne 0 ]; then cat "$WORK_DIR/cleanup.log" >&2; fi
    rm -rf "$WORK_DIR"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d falkordb
export ONTOLOGY_FALKOR_TEST_ADDRESS
ONTOLOGY_FALKOR_TEST_ADDRESS=$(compose port falkordb 6379)
case "$ONTOLOGY_FALKOR_TEST_ADDRESS" in
    127.0.0.1:*) ;;
    *) echo "Disposable FalkorDB must bind only loopback" >&2; exit 1 ;;
esac
export ADDP_ONTOLOGY_FALKOR_INTEGRATION=1
export GOWORK=off
cd "$ROOT_DIR/ontology/backend"
go test ./internal/falkor -run '^TestFalkorIntegration$' -count=1 -timeout=90s -v 2>&1 | tee "$WORK_DIR/tests.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then
    echo "Ontology FalkorDB gate refuses skipped tests" >&2
    exit 1
fi
