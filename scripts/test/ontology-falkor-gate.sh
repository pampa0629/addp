#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=falkordb
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ONTOLOGY_POSTGRES_TEST_DSN
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.ontology-falkor-t2.yml
# ADDP_T2_INPUT_FILES=docker-compose.infra.yml .env.example scripts/infra/falkordb.yml scripts/infra/up.sh scripts/infra/status.sh scripts/infra/down.sh scripts/prod/setup-env.sh scripts/prod/wait-infra.sh
# Own the complete lifecycle of a disposable FalkorDB, never a developer endpoint.
set -euo pipefail
if [ -z "${ONTOLOGY_POSTGRES_TEST_DSN:-}" ]; then
    echo "ONTOLOGY_POSTGRES_TEST_DSN is required for the PG/FalkorDB projection runtime gate" >&2
    exit 1
fi
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-ontology-falkor.XXXXXX")
RUN_ID=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
export ONTOLOGY_POSTGRES_TEST_RUN_ID
ONTOLOGY_POSTGRES_TEST_RUN_ID=$(python3 -c 'import sys,uuid; print(uuid.UUID(sys.argv[1]))' "$RUN_ID")
POSTGRES_STARTED=0
COMPOSE_PROJECT="addp-ontology-falkor-t2-$RUN_ID"
COMPOSE_FILE="$ROOT_DIR/scripts/test/docker-compose.ontology-falkor-t2.yml"
export ONTOLOGY_FALKOR_TEST_PASSWORD="$RUN_ID"
export INFRA_FALKORDB_PASSWORD="$RUN_ID"
compose() { docker compose --env-file /dev/null -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" "$@"; }
cleanup() {
    local result=$?
    trap - EXIT INT TERM
    set +e
    if [ "$POSTGRES_STARTED" = 1 ]; then
        (cd "$ROOT_DIR/ontology/backend" && go test ./internal/service -run '^TestPostgresGateCleanup$' -count=1 -timeout=30s -v) > "$WORK_DIR/postgres-cleanup.log" 2>&1 || {
            cat "$WORK_DIR/postgres-cleanup.log" >&2
            result=1
        }
    fi
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
compose up -d --wait --wait-timeout 60 falkordb
# Exercise the same persistence contract as Infra, with an owned disposable volume.
compose exec -T falkordb redis-cli --raw GRAPH.QUERY infra_persistence_probe \
  'CREATE (:Probe {value: "ontology-infra-recovered"})' timeout 2000 > "$WORK_DIR/probe.log"
compose exec -T falkordb redis-cli --raw SAVE | grep -qx OK
compose up -d --force-recreate --wait --wait-timeout 60 falkordb
compose exec -T falkordb redis-cli --raw GRAPH.RO_QUERY infra_persistence_probe \
  'MATCH (p:Probe) RETURN p.value' timeout 2000 | grep -qx ontology-infra-recovered
compose exec -T falkordb env -u REDISCLI_AUTH redis-cli --raw PING | grep -q NOAUTH
compose exec -T falkordb redis-cli --raw GRAPH.DELETE infra_persistence_probe > "$WORK_DIR/probe-cleanup.log"
echo "FalkorDB persistence and authentication probe passed"
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
POSTGRES_STARTED=1
go test ./internal/service -run '^TestPostgresProjectionRuntime$' -count=1 -timeout=90s -v 2>&1 | tee "$WORK_DIR/runtime-tests.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/runtime-tests.log"; then
    echo "Ontology projection runtime gate refuses skipped tests" >&2
    exit 1
fi
