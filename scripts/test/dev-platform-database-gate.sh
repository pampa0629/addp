#!/usr/bin/env bash
# Exercises the native development installer against its own Compose-labelled PostgreSQL.
set -euo pipefail
repo=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
ROOT_DIR=$(cd "$work" && pwd -P)
work=$ROOT_DIR
image=public.ecr.aws/docker/library/postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c
capability=keyword
if [[ "$#" != 0 ]]; then
  [[ "$#" = 2 && "$1" = --vector-image && "$2" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'expected --vector-image IMAGE_ID' >&2; exit 2; }
  image=$2
  capability=vector
fi
falkor_project=""
cleanup() {
  local result=$?
  if [[ -n "$falkor_project" ]]; then "${falkor[@]}" down -v >/dev/null || result=1; fi
  if [[ -s "$work/container.id" ]]; then docker rm -fv "$(cat "$work/container.id")" >/dev/null || result=1; fi
  rm -rf "$work"
  exit "$result"
}
trap cleanup EXIT
mkdir -p "$work"/{scripts/dev,scripts/infra,scripts/utils,.dev-bins}
cp "$repo/scripts/dev/platform-database.sh" "$work/scripts/dev/"
cp "$repo/scripts/infra/"{platform-database,ports}.sh "$work/scripts/infra/"
cp "$repo/scripts/utils/observability-env.sh" "$work/scripts/utils/"
source "$work/scripts/dev/platform-database.sh"
source "$work/scripts/infra/ports.sh"
# Discovery uses the isolated workspace label. Cleanup uses only the created ID.
unset ADDP_ONLINE_HOST ADDP_INFRA_DATABASE_PROVIDER ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
export POSTGRES_DB=addp_test POSTGRES_HOST=127.0.0.1 POSTGRES_USER=postgres
POSTGRES_PASSWORD=$(openssl rand -hex 24)
export POSTGRES_PASSWORD
docker run -d --cidfile "$work/container.id" --name "addp-dev-platform-postgres-$$" \
  --label com.docker.compose.project=addp-infra --label com.docker.compose.service=postgres \
  --label "com.docker.compose.project.working_dir=$ROOT_DIR" -e POSTGRES_DB -e POSTGRES_USER -e POSTGRES_PASSWORD \
  -p 127.0.0.1::5432 "$image" >/dev/null
pg() {
  PGPASSWORD="$POSTGRES_PASSWORD" docker exec -i -e PGPASSWORD "$(cat "$work/container.id")" \
    psql -XwAtq -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"
}
for ((i=0;i<90;i++)); do
  if [[ "$(pg -c 'SELECT current_database()' 2>/dev/null)" == addp_test ]]; then break; fi
  sleep 1
done
[[ "$(pg -c 'SELECT current_database()')" == addp_test ]]
addp_dev_select_database
[[ "$ADDP_INFRA_DATABASE_PROVIDER" == postgres ]]
POSTGRES_PORT=$(addp_infra_mapped_port "$(cat "$work/container.id")" 5432)
export POSTGRES_PORT
(cd "$repo/system/backend" && GOWORK=off go build -o "$work/.dev-bins/addp-system" ./cmd/server)
cd "$work"
addp_dev_prepare_database system
pg -c "SELECT version,dirty FROM system.schema_migrations"
PGOPTIONS='-c default_transaction_read_only=on' addp_dev_prepare_database system
# Manager has a useful keyword-only configuration on the stock PostgreSQL image.
(cd "$repo/manager/backend" && GOWORK=off go build -o "$work/.dev-bins/addp-manager" ./cmd/server)
ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
export ADDP_DATABASE_RUNTIME_USER=platform_run_gate ADDP_DATABASE_RUNTIME_PASSWORD
pg <<SQL
CREATE ROLE platform_run_gate LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
CREATE SCHEMA manager AUTHORIZATION postgres;
CREATE SCHEMA security AUTHORIZATION postgres;
GRANT USAGE ON SCHEMA common,manager,security TO platform_run_gate;
GRANT SELECT ON ALL TABLES IN SCHEMA common TO platform_run_gate;
ALTER DEFAULT PRIVILEGES IN SCHEMA manager,security GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO platform_run_gate;
ALTER DEFAULT PRIVILEGES IN SCHEMA manager,security GRANT USAGE,SELECT ON SEQUENCES TO platform_run_gate;
SQL
check_manager() (
  unset POSTGRES_USER POSTGRES_PASSWORD ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
  cd "$repo/manager/backend"
  ADDP_TEST_MANAGER_PREPARED_RUNTIME="$1" GOWORK=off go test ./internal/repository -run '^TestManagerPreparedRuntime$' -count=1 -v
)
check_manager missing
addp_dev_prepare_database manager
check_manager "$capability"
PGOPTIONS='-c default_transaction_read_only=on' addp_dev_prepare_database manager
check_manager "$capability"
if [[ "$capability" = vector ]]; then
  (cd "$repo/manager/backend" && ADDP_POSTGRES_INTEGRATION=1 \
    MANAGER_POSTGRES_TEST_DSN="postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@127.0.0.1:$POSTGRES_PORT/$POSTGRES_DB?sslmode=disable&search_path=manager,public" \
    GOWORK=off go test ./internal/repository -run '^TestIntegrationPostgresManagerSearchSchemaAndVectorBatches$' -count=1 -v)
fi
echo 'Manager native database preparation and two restricted runtime openings passed.'
(cd "$repo/security/backend" && GOWORK=off go build -o "$work/.dev-bins/addp-security" ./cmd/server)
check_security() (
  unset POSTGRES_USER POSTGRES_PASSWORD ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
  cd "$repo/security/backend"
  ADDP_TEST_SECURITY_PREPARED_RUNTIME="$1" GOWORK=off go test ./internal/repository -run '^TestSecurityPreparedRuntime$' -count=1 -v
)
check_security missing
addp_dev_prepare_database security
check_security ready
PGOPTIONS='-c default_transaction_read_only=on' addp_dev_prepare_database security
check_security ready
echo 'Security native installation and two restricted runtime openings passed.'

# A legacy workspace can own the schema and its data without runtime grants.
pg_runtime() {
  PGPASSWORD="$ADDP_DATABASE_RUNTIME_PASSWORD" docker exec -i -e PGPASSWORD "$(cat "$work/container.id")" \
    psql -XwAtq -h 127.0.0.1 -U "$ADDP_DATABASE_RUNTIME_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 "$@"
}
export INFRA_FALKORDB_PASSWORD=$(openssl rand -hex 24)
cat >"$work/falkor.yml" <<'YAML'
services:
  falkordb:
    ports: ["127.0.0.1::6379"]
YAML
falkor=(docker compose -p "addp-dev-platform-falkor-$$" -f "$repo/scripts/infra/falkordb.yml" -f "$work/falkor.yml")
pg <<'SQL'
CREATE SCHEMA unrelated;
CREATE TABLE unrelated.private_record (id BIGINT PRIMARY KEY);
SQL
for owner in develop monitor ontology quality transfer; do
  (cd "$repo/$owner/backend" && GOWORK=off go build -o "$work/$owner" ./cmd/server)
  ln -s "$work/$owner" "$work/.dev-bins/addp-$owner"
  pg <<SQL
CREATE SCHEMA $owner AUTHORIZATION postgres;
CREATE TABLE $owner.upgrade_probe (id BIGSERIAL PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO $owner.upgrade_probe (value) VALUES ('legacy');
SQL
  if pg_runtime -c "SELECT value FROM $owner.upgrade_probe" >"$work/old-permissions.log" 2>&1; then exit 1; fi
  grep -F "permission denied for schema $owner" "$work/old-permissions.log"
  addp_dev_prepare_database "$owner"
  [[ $(pg_runtime -c "SELECT version > 0 FROM $owner.startup_schema_revision") == t ]]
  [[ $(pg_runtime -c "SELECT value FROM $owner.upgrade_probe") == legacy ]]
  [[ $(pg_runtime -c "SELECT NOT has_schema_privilege('unrelated','USAGE')") == t ]]
  [[ $(pg_runtime -c "SELECT NOT has_schema_privilege('$owner','CREATE') AND NOT has_table_privilege('$owner.startup_schema_revision','UPDATE')") == t ]]
  pg_runtime <<SQL
BEGIN;
INSERT INTO $owner.upgrade_probe (value) VALUES ('new');
UPDATE $owner.upgrade_probe SET value='changed' WHERE value='new';
DELETE FROM $owner.upgrade_probe WHERE value='changed';
ROLLBACK;
SQL
  PGOPTIONS='-c default_transaction_read_only=on' addp_dev_prepare_database "$owner"
  if [[ "$owner" == ontology ]]; then
    falkor_project="addp-dev-platform-falkor-$$"
    "${falkor[@]}" up -d --wait
    export INFRA_FALKORDB_ADDRESS=$("${falkor[@]}" port falkordb 6379)
  fi
  python3 "$repo/scripts/test/module-platform-startup.py" "$work/$owner" ready
  echo "$owner native upgrade preserved legacy data and provided runtime access."
done

pg -c 'DELETE FROM system.schema_migration_checksums WHERE version=37'
if addp_dev_prepare_database system >"$work/missing.log" 2>&1; then exit 1; fi
grep -F '000037 is missing' "$work/missing.log"
[[ "$(pg -c 'SELECT count(*) FROM system.schema_migration_checksums WHERE version=37')" == 0 ]]
ADDP_DATABASE_DEPLOY_USER="$POSTGRES_USER" ADDP_DATABASE_DEPLOY_PASSWORD="$POSTGRES_PASSWORD" \
  "$work/.dev-bins/addp-system" prepare-database system
PGOPTIONS='-c default_transaction_read_only=on' addp_dev_prepare_database system
echo 'Verified native preparation, read-only repeat, missing checksum rejection and explicit recovery.'
