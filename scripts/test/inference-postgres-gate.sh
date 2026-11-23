#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=postgres
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.inference-postgres-t2.yml
# ADDP_T2_INPUT_FILES=scripts/test/inference-platform-startup.py common/infra/bootstrap/ common/schema/
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
project="addp-inference-disposable-$(openssl rand -hex 6)"
export ADDP_INFRA_DATABASE_PROVIDER=postgres ADDP_DATABASE_DEPLOY_USER=inference_deploy
ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD ADDP_DATABASE_RUNTIME_USER=inference_fixture_runtime
compose() {
    docker compose --env-file /dev/null -p "$project" -f "$root/scripts/test/docker-compose.inference-postgres-t2.yml" "$@"
}
cleanup() {
    local status=$?
    compose down --volumes --remove-orphans || status=1
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT
compose up -d --wait postgres
export POSTGRES_HOST=127.0.0.1 POSTGRES_DB=addp_inference_disposable
mapping=$(compose port postgres 5432)
export POSTGRES_PORT="${mapping##*:}"
cd "$root/inference/backend"
GOWORK=off go build -o "$work/inference" ./cmd/server
compose exec -T postgres psql -Xw -v ON_ERROR_STOP=1 -U "$ADDP_DATABASE_DEPLOY_USER" -d "$POSTGRES_DB" <<SQL
CREATE ROLE inference_fixture_runtime LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
CREATE SCHEMA inference;
GRANT USAGE ON SCHEMA inference TO inference_fixture_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA inference GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO inference_fixture_runtime;
SQL
python3 "$root/scripts/test/inference-platform-startup.py" "$work/inference" unprepared
"$work/inference" prepare-database
"$work/inference" prepare-database
python3 "$root/scripts/test/inference-platform-startup.py" "$work/inference"
ADDP_TEST_INFERENCE_PLATFORM=1 GOWORK=off go test ./internal/repository -run '^TestInferencePlatformStore$' -count=1 -v | tee "$work/result.log"
grep -q '^--- PASS: TestInferencePlatformStore ' "$work/result.log"
