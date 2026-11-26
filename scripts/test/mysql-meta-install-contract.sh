#!/usr/bin/env bash
# Exercise both deployment commands in the gate's disposable MySQL instance.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$1
container=$2
export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3" ADDP_INFRA_DATABASE_PROVIDER=mysql
admin() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB meta'
}
system_password=$(openssl rand -hex 24)
admin <<SQL
CREATE DATABASE \`system\`;
CREATE USER 'system_setup'@'%' IDENTIFIED BY '$system_password';
GRANT ALL ON \`system\`.* TO 'system_setup'@'%' WITH GRANT OPTION;
CREATE USER 'worker_gate'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT SELECT,INSERT,UPDATE,DELETE ON meta.* TO 'worker_gate'@'%';
GRANT ALL ON meta.* TO 'system_setup'@'%' WITH GRANT OPTION;

SQL

export ADDP_DATABASE_DEPLOY_USER=system_setup ADDP_DATABASE_DEPLOY_PASSWORD="$system_password"
result=0
"$work/system-server" prepare-database system >"$work/system-triggers.sql" || result=$?
test "$result" = 3
admin <"$work/system-triggers.sql"
"$work/system-server" prepare-database system

# Exercise the real address key including platform scope, JSON object equality,
# case-insensitive engine type and release of an address by a permanent tombstone.
admin <<'SQL'
INSERT INTO `system`.engines(id,tenant_id,name,engine_type,connection_info,identity_key)
VALUES (99,7,'scan','mysql','{}','{"host":"scan","port":"3306"}'),
       (100,8,'sentinel','mysql','{}','{"port":"3306","host":"scan"}'),
       (101,NULL,'platform','mysql','{}','{"host":"scan","port":"3306"}');
SQL
for tenant in 7 NULL; do
    if echo "INSERT INTO \`system\`.engines(tenant_id,name,engine_type,connection_info,identity_key) VALUES ($tenant,'duplicate','MYSQL','{}','{\"port\":\"3306\",\"host\":\"scan\"}')" | admin 2>"$work/address.err"; then exit 1; fi
    grep -q 'ERROR 1062' "$work/address.err"
done
admin <<'SQL'
UPDATE `system`.engines SET lifecycle_state='deleted', deleted_at=NOW(6) WHERE id=101;
INSERT INTO `system`.engines(tenant_id,name,engine_type,connection_info,identity_key)
VALUES (NULL,'new platform','mysql','{}','{"host":"scan","port":"3306"}');
CREATE DATABASE scan_source;
CREATE TABLE scan_source.orders (id BIGINT PRIMARY KEY, label VARCHAR(30));
INSERT INTO scan_source.orders VALUES (1,'sample');
SQL
admin <<SQL
CREATE USER 'scan_reader'@'%' IDENTIFIED BY '$ADDP_TEST_META_SOURCE_PASSWORD';
GRANT SELECT ON scan_source.* TO 'scan_reader'@'%';
SQL
result=0
"$work/meta-server" prepare-database >"$work/meta-triggers.sql" || result=$?
test "$result" = 3
admin <<'SQL'
INSERT INTO `system`.engines(id,tenant_id,name,engine_type,connection_info,identity_key)
VALUES (201,7,'node fk','mysql','{}','{"host":"fk-node"}'),(202,8,'item fk','mysql','{}','{"host":"fk-item"}');
INSERT INTO meta_node(tenant_id,engine_id,node_type,name,depth) VALUES(7,201,'root','fk gate',0);
INSERT INTO meta_item(tenant_id,engine_id,node_id,item_type,name,fingerprint,attributes)
VALUES(8,202,1,'table','fk gate','fk-gate-item','{"storage":{"schema_name":"orders"},"type_info":{"table":{"fields":[{"name":"amount","type":"int"}]}}}');
INSERT INTO lineage_service_dependencies(tenant_id,source_item_id,service_id,published_revision,dependency_kind,first_observed_at,last_observed_at,service_updated_at)
SELECT 8,id,202,'prior','table',NOW(),NOW(),'2026-01-01' FROM meta_item WHERE fingerprint='fk-gate-item';
SQL
admin <"$work/meta-triggers.sql"
"$work/meta-server" prepare-database
test "$(echo "SELECT COUNT(*) FROM lineage_service_publication_times WHERE tenant_id=8 AND service_id=202 AND service_updated_at='2026-01-01'" | admin)" = 1
echo "DELETE FROM lineage_service_dependencies WHERE tenant_id=8 AND service_id=202" | admin
# DBA-owned triggers are verified again on installation replay.
echo 'DROP TRIGGER cleanup_result_immutable' | admin
result=0
"$work/meta-server" prepare-database >"$work/meta-restore-trigger.sql" || result=$?
test "$result" = 3
test "$(grep -c '^CREATE DEFINER=' "$work/meta-restore-trigger.sql")" = 1
grep -q ' TRIGGER IF NOT EXISTS cleanup_result_immutable' "$work/meta-restore-trigger.sql"
admin <"$work/meta-restore-trigger.sql"
"$work/meta-server" prepare-database
# Run the real upgrade on populated v1 tables. Separate engines prove each FK independently.
admin <<'SQL'
ALTER TABLE meta_node DROP FOREIGN KEY fk_meta_node_engine_restrict, ADD CONSTRAINT fk_meta_node_tenant_engine
    FOREIGN KEY (tenant_id,engine_id) REFERENCES `system`.engines(tenant_id,id) ON DELETE CASCADE;
ALTER TABLE meta_item DROP FOREIGN KEY fk_meta_item_engine_restrict, ADD CONSTRAINT fk_meta_item_tenant_engine
    FOREIGN KEY (tenant_id,engine_id) REFERENCES `system`.engines(tenant_id,id) ON DELETE CASCADE;
DROP TABLE receipt_scan_locks;
UPDATE startup_schema_revision SET version=1 WHERE owner='meta_scan_tasks';
UPDATE startup_schema_revision SET version=1 WHERE owner='meta_scan_catalog';
SQL
"$work/meta-server" prepare-database
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner='meta_scan_tasks' AND version=2 AND pending_version=0" | admin)" = 1
test "$(echo 'SELECT COUNT(*) FROM receipt_scan_locks' | admin)" = 0
for entry in '201 meta_node' '202 meta_item'; do
    read -r id table <<<"$entry"
    if echo "DELETE FROM \`system\`.engines WHERE id=$id" | admin 2>"$work/meta-delete.err"; then exit 1; fi
    grep -q "ERROR 1451.*fk_${table}_engine_restrict" "$work/meta-delete.err"
done
echo 'Both populated engine references rejected physical deletion (1451)'
ADDP_TEST_META_CLEANUP_PASSWORD="$ADDP_DATABASE_RUNTIME_PASSWORD" GOWORK=off go -C "$root/meta/backend" test ./internal/service \
    -run '^TestMetaCleanupChangesAgainstMySQL$' -count=1 -v | tee "$work/meta-cleanup.log"
grep -q '^--- PASS: TestMetaCleanupChangesAgainstMySQL ' "$work/meta-cleanup.log"
test "$(echo 'DELETE FROM `system`.engines WHERE id IN (201,202); SELECT ROW_COUNT()' | admin)" = 2
echo 'Both parent deletions committed after Meta child cleanup'
# An already published revision must not hide a missing key on installation replay.
admin <<'SQL'
ALTER TABLE meta_item DROP FOREIGN KEY fk_meta_item_engine_restrict;
SQL
if "$work/meta-server" prepare-database 2>"$work/meta-key.err"; then exit 1; fi
grep -q 'requires both tenant/engine foreign keys' "$work/meta-key.err"
MYSQL_PWD="$system_password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -usystem_setup meta <<'SQL'
ALTER TABLE meta_item ADD CONSTRAINT fk_meta_item_engine_restrict FOREIGN KEY (tenant_id, engine_id)
    REFERENCES `system`.engines(tenant_id, id) ON DELETE RESTRICT;
SQL
"$work/meta-server" prepare-database
echo 'Installed both Meta foreign keys; populated parent rejected and cleaned parent deletable.'
