#!/usr/bin/env bash
# Called by the gate that owns the MySQL instance and the prebuilt commands.
set -euo pipefail
work=$1
container=$2
export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3" ADDP_INFRA_DATABASE_PROVIDER=mysql
export ADDP_DATABASE_DEPLOY_USER=platform_deploy_gate
export ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
export ADDP_DATABASE_RUNTIME_USER=platform_runtime_gate
export ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
admin() {
    docker exec -i "$container" sh -c 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB common'
}
admin <<SQL
CREATE DATABASE \`system\`;
CREATE DATABASE meta;
CREATE USER 'platform_deploy_gate'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
GRANT ALL ON common.* TO 'platform_deploy_gate'@'%';
GRANT ALL ON \`system\`.* TO 'platform_deploy_gate'@'%';
SQL
"$work/system-server" prepare-database common
"$work/system-server" prepare-database common
version=$(echo "SELECT version FROM startup_schema_revision WHERE owner='common' AND pending_version=0" | admin)
test "$version" -gt 0
test "$(echo "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='common' AND table_name IN ('task_executions','execution_events','background_runtime_heartbeats')" | admin)" = 3
# Matching revisions in the local module databases must never mask missing Common state.
admin <<SQL
CREATE USER 'platform_runtime_gate'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
CREATE TABLE \`system\`.startup_schema_revision LIKE common.startup_schema_revision;
CREATE TABLE meta.startup_schema_revision LIKE common.startup_schema_revision;
INSERT INTO \`system\`.startup_schema_revision SELECT * FROM common.startup_schema_revision;
INSERT INTO meta.startup_schema_revision SELECT * FROM common.startup_schema_revision;
GRANT SELECT ON \`system\`.startup_schema_revision TO 'platform_runtime_gate'@'%';
GRANT SELECT ON meta.startup_schema_revision TO 'platform_runtime_gate'@'%';
GRANT SELECT ON common.startup_schema_revision TO 'platform_runtime_gate'@'%';
SQL
verify() {
    ADDP_TEST_MYSQL_COMMON_STARTUP="$1" "$work/common-startup.test" -test.run '^TestRestrictedCommonStartupAgainstMySQL$' -test.v >"$work/check-$1.log" || { cat "$work/check-$1.log"; return 1; }
    for owner in system meta; do grep -q -- "--- PASS: TestRestrictedCommonStartupAgainstMySQL/$owner " "$work/check-$1.log"; done
}
verify ready
echo "UPDATE startup_schema_revision SET pending_version=version+1 WHERE owner='common'" | admin
verify pending
echo "UPDATE startup_schema_revision SET pending_version=0,version=version+1 WHERE owner='common'" | admin
verify mismatch
echo "DELETE FROM startup_schema_revision WHERE owner='common'" | admin
verify missing
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision" | admin)" = 0
echo 'Common installation verified: deployment account, execution/events/health tables and shared runtime cross-database revision checks'
