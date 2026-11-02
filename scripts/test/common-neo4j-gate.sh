#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=neo4j
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.neo4j-t2.yml
# ADDP_T2_INPUT_FILES=common/engine/propertygraph/ common/engine/plugin/property_graph.go graph/backend/ common/engine/plugins/neo4j/ common/dataprotection/projectionstore/ develop/backend/internal/protection/ develop/backend/internal/service/dev_executor.go scripts/ci/check-t2-ci-registration_test.py scripts/ci/check-t2-ci-registration.py scripts/test/module-gate.py
# Own a disposable deployment; never reuse a business engine.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-neo4j-t2.XXXXXX")
COMPOSE_PROJECT="addp-neo4j-t2-${PPID}-$$"
export NEO4J_T2_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
export NEO4J_T2_BOLT_PORT=0
compose() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.neo4j-t2.yml" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans >"$WORK_DIR/cleanup.log" 2>&1 || status=1
    local containers volumes networks
    if ! containers=$(docker ps -a --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}') || ! volumes=$(docker volume ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.Name}}') || ! networks=$(docker network ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}'); then
        echo 'Neo4j T2 cleanup could not verify resources' >&2; status=1
    elif [ -n "$containers$volumes$networks" ]; then
        echo 'Neo4j T2 cleanup left resources' >&2; status=1
    else
        echo 'NEO4J_T2_CLEANUP zero_residuals'
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
start_neo4j() {
    local startup_status
    if compose up -d --wait --wait-timeout 240; then
        return
    else
        startup_status=$?
    fi
    # Inspect only this Compose service before cleanup removes the evidence.
    # Emit no container environment; redact the generated fixture credential.
    {
        compose logs --no-color --tail 200 neo4j || true
        container_id=$(compose ps -a -q neo4j) || container_id=""
        if [ -n "$container_id" ]; then
            docker inspect --format '{"status":{{json .State.Status}},"exit_code":{{.State.ExitCode}},"oom_killed":{{.State.OOMKilled}},"error":{{json .State.Error}}}' "$container_id" || true
        fi
    } 2>&1 | python3 -c 'import os,sys; value=sys.stdin.read(); secret=os.environ.get("NEO4J_T2_PASSWORD", ""); sys.stdout.write(value.replace(secret, "[redacted]") if secret else value)' >&2 || true
    return "$startup_status"
}
start_neo4j
export ADDP_NEO4J_INTEGRATION=1
export NEO4J_T2_ENDPOINT="$(compose port neo4j 7687)"
(cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/neo4j -run '^TestIntegrationNeo4j' -count=1 -v) 2>&1 | tee "$WORK_DIR/tests.log"
(cd "$ROOT_DIR/graph/backend" && GOWORK=off go test ./internal/service -run '^TestIntegrationNeo4jGraphDomain$' -count=1 -v) 2>&1 | tee -a "$WORK_DIR/tests.log"
grep -q -- '--- PASS: TestIntegrationNeo4jGraphDomain ' "$WORK_DIR/tests.log" || { echo 'Neo4j gate requires Graph domain contract test' >&2; exit 1; }

export NEO4J_T2_IDENTITY_DIR="$WORK_DIR"
identity_phase() {
    export NEO4J_T2_IDENTITY_PHASE="$1"
    (cd "$ROOT_DIR/common" && GOWORK=off go test ./engine/plugins/neo4j -run '^TestNativeNeo4jTargetIdentityLifecycle$' -count=1 -v) 2>&1 | tee -a "$WORK_DIR/tests.log"
    grep -q "NEO4J_IDENTITY_PHASE_PASS phase=$1 version=5.26.24 edition=community" "$WORK_DIR/tests.log" || { echo "Neo4j identity phase missing: $1" >&2; exit 1; }
}
owned_volume() {
    local binding owner volume
    binding=$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}|{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}' "$1")
    IFS='|' read -r owner volume <<< "$binding"
    [ "$owner" = "$COMPOSE_PROJECT" ] && [ "$volume" = "${COMPOSE_PROJECT}_neo4j_data" ] || { echo 'Neo4j identity requires its exact owned data volume' >&2; return 1; }
    echo "$volume"
}
same_endpoint() {
    [ "$(compose port neo4j 7687)" = "$NEO4J_T2_ENDPOINT" ] || { echo 'Neo4j identity endpoint changed' >&2; return 1; }
}
# Docker can allocate a new published port when a port-0 container restarts.
# Pin this run's actual allocation before observing the identity baseline.
compose stop neo4j
export NEO4J_T2_BOLT_PORT="${NEO4J_T2_ENDPOINT##*:}"
start_neo4j
same_endpoint
original_container=$(compose ps -q neo4j)
original_volume=$(owned_volume "$original_container")
original_volume_created=$(docker volume inspect --format '{{.CreatedAt}}' "$original_volume")
identity_phase baseline
compose stop neo4j
start_neo4j
[ "$(compose ps -q neo4j)" = "$original_container" ]
same_endpoint
identity_phase restart

# The archive is copied off the owned volume before that volume is destroyed.
compose stop neo4j
compose run --rm --no-deps -T neo4j sh -eu -c 'mkdir -p /data/identity-dump; exec neo4j-admin database dump neo4j --to-path=/data/identity-dump'
compose cp neo4j:/data/identity-dump/neo4j.dump "$WORK_DIR/original.dump"
chmod 600 "$WORK_DIR/original.dump"
[ -s "$WORK_DIR/original.dump" ]
# Include the system store in a consistent offline snapshot of this fixture only.
compose cp neo4j:/data/. "$WORK_DIR/full-store"
[ -d "$WORK_DIR/full-store/databases/system" ]
start_neo4j
[ "$(compose ps -q neo4j)" = "$original_container" ]
same_endpoint
identity_phase post-dump
compose down --volumes --remove-orphans
if docker volume inspect "$original_volume" >/dev/null 2>&1; then
    echo 'Neo4j identity original storage was not destroyed' >&2; exit 1
fi

# Keep the pinned endpoint for the new storage; a collision fails closed.
start_neo4j
same_endpoint
replacement_container=$(compose ps -q neo4j)
[ "$replacement_container" != "$original_container" ]
replacement_volume=$(owned_volume "$replacement_container")
[ "$(docker volume inspect --format '{{.CreatedAt}}' "$replacement_volume")" != "$original_volume_created" ]
identity_phase recreated
compose stop neo4j
compose run --rm --no-deps -T neo4j sh -eu -c 'umask 077; mkdir -p /data/identity-restore; cat > /data/identity-restore/neo4j.dump; exec neo4j-admin database load neo4j --from-path=/data/identity-restore --overwrite-destination=true' < "$WORK_DIR/original.dump"
start_neo4j
[ "$(compose ps -q neo4j)" = "$replacement_container" ]
same_endpoint
identity_phase restored
compose stop neo4j
start_neo4j
[ "$(compose ps -q neo4j)" = "$replacement_container" ]
same_endpoint
identity_phase restored-restart

# A complete storage clone also tests whether instance/database IDs add evidence.
compose down --volumes --remove-orphans
if docker volume inspect "$replacement_volume" >/dev/null 2>&1; then
    echo 'Neo4j identity replacement storage was not destroyed' >&2; exit 1
fi
start_neo4j
same_endpoint
clone_container=$(compose ps -q neo4j)
[ "$clone_container" != "$replacement_container" ]
clone_volume=$(owned_volume "$clone_container")
identity_phase clone-recreated
compose stop neo4j
compose run --rm --no-deps -T neo4j sh -eu -c 'find /data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +'
compose cp "$WORK_DIR/full-store/." neo4j:/data
start_neo4j
[ "$(compose ps -q neo4j)" = "$clone_container" ]
[ "$(owned_volume "$clone_container")" = "$clone_volume" ]
same_endpoint
identity_phase full-clone
compose stop neo4j
start_neo4j
[ "$(compose ps -q neo4j)" = "$clone_container" ]
same_endpoint
identity_phase full-clone-restart
grep -q 'NEO4J_IDENTITY_COUNTEREXAMPLE candidate_preserved=true original_storage_destroyed=true post_dump_data_absent=true' "$WORK_DIR/tests.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/tests.log"; then echo 'Neo4j T2 refuses skipped tests' >&2; exit 1; fi
grep -q -- '--- PASS: TestIntegrationNeo4j' "$WORK_DIR/tests.log"
echo 'NEO4J_T2_PASS local read scope, prepared graph, bounded results, identity restore counterexample and rejection boundaries'
