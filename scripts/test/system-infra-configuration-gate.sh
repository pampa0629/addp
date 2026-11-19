#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=postgres,redis
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.system-infra-configuration-t2.yml
# ADDP_T2_INPUT_FILES=docker-compose.infra.yml docker-compose.yml scripts/infra/infra-postgresql.conf scripts/infra/redis-authentication.sh system/backend/internal/infraconfig/ system/backend/internal/api/infra_configuration.go system/backend/internal/api/infra_configuration_routes.go system/backend/internal/api/infra_configuration_test.go system/backend/internal/iam/audit_writer.go system/backend/internal/config/config.go
# Own a disposable Compose project for cluster-wide changes.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PROJECT="addp-infra-config-t2-$(python3 -c 'import uuid; print(uuid.uuid4().hex)')"
export ADDP_INFRA_GATE_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
export ADDP_INFRA_GATE_MANAGER_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
compose() {
    docker compose --env-file /dev/null -p "$PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.system-infra-configuration-t2.yml" "$@"
}
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans || status=1
    local containers volumes networks
    containers=$(docker ps -aq --filter "label=com.docker.compose.project=$PROJECT") || status=1
    volumes=$(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT") || status=1
    networks=$(docker network ls -q --filter "label=com.docker.compose.project=$PROJECT") || status=1
    if [ -n "$containers$volumes$networks" ]; then
        echo "Infra configuration T2 cleanup left owned resources" >&2
        status=1
    fi
    if [ "$status" -eq 0 ]; then
        echo "Infra configuration T2 cleanup: zero owned containers, volumes and networks"
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
compose up -d --wait --wait-timeout 100
compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U infra_test -d addp_test <<SQL
CREATE ROLE addp_infra_config LOGIN PASSWORD '$ADDP_INFRA_GATE_MANAGER_PASSWORD';
GRANT ALTER SYSTEM ON PARAMETER work_mem, maintenance_work_mem, log_min_duration_statement, log_lock_waits TO addp_infra_config;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_reload_conf() TO addp_infra_config;
SQL
run_case() {
    local phase=$1 package=${2:-./internal/infraconfig} test=${3:-TestInfraConfigurationNative}
    local pg_endpoint redis_endpoint result
    pg_endpoint=$(compose port postgres 5432)
    redis_endpoint=$(compose port redis 6379)
    cd "$ROOT_DIR/system/backend"
    result=$(ADDP_INFRA_CONFIGURATION_INTEGRATION=1 ADDP_INFRA_GATE_PHASE="$phase" \
        ADDP_INFRA_GATE_PG_PORT="${pg_endpoint##*:}" ADDP_INFRA_GATE_REDIS_PORT="${redis_endpoint##*:}" \
        GOWORK=off go test "$package" -run "^$test$" -count=1 -v 2>&1) \
        || { printf '%s\n' "$result" >&2; return 1; }
    printf '%s\n' "$result"
    grep -Fq -- "--- PASS: $test " <<< "$result"
}
run_case apply
compose exec -T --user root redis chmod 0555 /data
run_case persistence-failure
compose exec -T --user root redis chmod 0755 /data
compose up -d --wait --wait-timeout 100 --force-recreate
run_case restart
run_case audit ./internal/api TestInfraConfigurationAudit
