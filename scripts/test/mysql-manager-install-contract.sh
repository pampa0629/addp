#!/usr/bin/env bash
# Called only from the gate that owns the loopback MySQL service.
set -euo pipefail
work=$1
container=$2
export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3" ADDP_INFRA_DATABASE_PROVIDER=mysql
export ADDP_DATABASE_DEPLOY_USER=manager_deploy_gate ADDP_DATABASE_RUNTIME_USER=manager_run_gate
ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD
admin() {
    docker exec -i "$container" sh -c 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB'
}
# The fixture has prepared Common execution facts and an empty Manager database.
admin <<SQL
CREATE USER 'manager_deploy_gate'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'manager_run_gate'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT ALL ON common.* TO 'manager_deploy_gate'@'%';
GRANT ALL ON manager.* TO 'manager_deploy_gate'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON manager.* TO 'manager_run_gate'@'%';
GRANT SELECT,INSERT,UPDATE ON common.* TO 'manager_run_gate'@'%';
SQL
"$work/system-server" prepare-database common
root=$(cd "$(dirname "$0")/../.." && pwd)
runtime_probe() (
    unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
    cd "$root/manager/backend"
    ADDP_TEST_MANAGER_PREPARED_RUNTIME="$1" GOWORK=off go test ./internal/repository -run '^TestManagerPreparedRuntime$' -count=1 -v
)
status=0
"$work/manager-backend" prepare-database > "$work/manager-preparation.sql" || status=$?
if [ "$status" = 3 ]; then
    test "$(echo "SELECT version,pending_version FROM manager.startup_schema_revision WHERE owner='manager'" | admin)" = $'0\t1'
    runtime_probe missing
    admin < "$work/manager-preparation.sql"
    "$work/manager-backend" prepare-database
elif [ "$status" != 0 ]; then
    exit "$status"
fi
test "$(echo "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema='manager' AND trigger_name IN ('search_index_insert','search_index_update','search_index_delete','cleanup_result_immutable')" | admin)" = 4
test "$(echo "SELECT version,pending_version FROM manager.startup_schema_revision WHERE owner='manager'" | admin)" = $'1\t0'
schema_digest() {
    docker exec "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysqldump --no-data --skip-comments --skip-dump-date --skip-lock-tables --no-tablespaces -uroot manager' | sed -E 's/ AUTO_INCREMENT=[0-9]+//g' | shasum -a 256
}
before=$(schema_digest)
runtime_probe keyword
"$work/manager-backend" prepare-database
runtime_probe keyword
test "$(schema_digest)" = "$before"
echo 'Manager MySQL installation verified: trigger review, restricted starts and unchanged table definitions.'
