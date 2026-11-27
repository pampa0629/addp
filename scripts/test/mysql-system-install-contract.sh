#!/usr/bin/env bash
# Called only by the gate that owns this disposable MySQL container.
set -euo pipefail
work=$1
container=$2
export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3" ADDP_INFRA_DATABASE_PROVIDER=mysql
export ADDP_DATABASE_DEPLOY_USER=iam_installer
export ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
runtime_password=$(openssl rand -hex 24)
export ADDP_DATABASE_RUNTIME_USER=iam_runtime ADDP_DATABASE_RUNTIME_PASSWORD="$runtime_password"
admin() {
    docker exec -i "$container" sh -c 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB system'
}
runtime() {
    MYSQL_PWD="$runtime_password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -uiam_runtime -NB system
}
admin <<SQL
CREATE USER 'iam_installer'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'iam_runtime'@'%' IDENTIFIED BY '$runtime_password';
GRANT ALL ON \`system\`.* TO 'iam_installer'@'%' WITH GRANT OPTION;
SQL
test "$(echo 'SELECT @@log_bin, @@log_bin_trust_function_creators' | admin)" = $'1\t0'
# An unknown argument must not launch the HTTP server or perform installation.
result=0
"$work/system-server" install-mysql-iam > "$work/retired.out" 2> "$work/retired.err" || result=$?
test "$result" = 2
test ! -s "$work/retired.out"
result=0
"$work/system-server" prepare-database system > "$work/triggers.sql" || result=$?
test "$result" = 3
test "$(echo "SELECT version=0 AND pending_version>0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | admin)" = 1
expected=$(grep -c '^CREATE DEFINER=' "$work/triggers.sql")
test "$expected" -gt 0
for name in module_node_declaration_immutable process_metrics_declaration_immutable cleanup_request_immutable execution_authorization_validate_insert execution_engine_access_validate_insert; do
    grep -q "TRIGGER IF NOT EXISTS $name$" "$work/triggers.sql"
done
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner IN ('system_iam_foundation','system_module_registry') AND version=0 AND pending_version>0" | admin)" = 2
test "$(echo "SELECT version=1 AND pending_version=0 FROM startup_schema_revision WHERE owner='system_access_request_locks'" | admin)" = 1
# A same-name object with a different body must not authorize startup.
admin <<'SQL'
CREATE DEFINER='iam_installer'@'%' TRIGGER trg_principals_updated_at BEFORE UPDATE ON principals FOR EACH ROW SET NEW.updated_at = OLD.updated_at;
SQL
result=0
"$work/system-server" prepare-database system > "$work/refused.sql" 2>"$work/refused.err" || result=$?
test "$result" = 1
grep -q 'trg_principals_updated_at differs' "$work/refused.err"
echo 'DROP TRIGGER trg_principals_updated_at' | admin
admin < "$work/triggers.sql"
# A failure recording completion must also roll back all bootstrap identities and grants.
admin <<'SQL'
DELIMITER $$
CREATE TRIGGER fixture_refuse_completion BEFORE UPDATE ON startup_schema_revision FOR EACH ROW
BEGIN
    IF NEW.pending_version=0 THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture rejected revision completion';
    END IF;
END$$
DELIMITER ;
SQL
result=0
"$work/system-server" prepare-database system > "$work/incomplete.out" 2>"$work/incomplete.err" || result=$?
test "$result" = 1
grep -q 'fixture rejected revision completion' "$work/incomplete.err"
test "$(echo 'SELECT (SELECT COUNT(*) FROM principals)+(SELECT COUNT(*) FROM oauth_clients)+(SELECT COUNT(*) FROM roles)+(SELECT COUNT(*) FROM permissions)+(SELECT COUNT(*) FROM role_assignments)+(SELECT COUNT(*) FROM role_permissions)' | admin)" = 0
test "$(echo "SELECT version=0 AND pending_version>0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | admin)" = 1
echo 'DROP TRIGGER fixture_refuse_completion' | admin
# The DBA may apply a partial list. Completed owners remain intact on replay.
echo 'DROP TRIGGER process_metrics_declaration_immutable' | admin
result=0
"$work/system-server" prepare-database system > "$work/last-trigger.sql" || result=$?
test "$result" = 3
test "$(grep -c '^CREATE DEFINER=' "$work/last-trigger.sql")" = 1
grep -q 'TRIGGER IF NOT EXISTS process_metrics_declaration_immutable$' "$work/last-trigger.sql"
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner IN ('system_access_request_locks','system_iam_foundation') AND version>0 AND pending_version=0" | admin)" = 2
test "$(echo 'SELECT COUNT(*) FROM module_registry_state' | admin)" = 0
admin < "$work/last-trigger.sql"
"$work/system-server" prepare-database system > "$work/remaining.sql"
test ! -s "$work/remaining.sql"
# This gate owns the entire instance. A healthy repeat must not issue DDL or grants.
schema_commands() {
    admin <<<"SHOW GLOBAL STATUS WHERE Variable_name IN ('Com_grant','Com_create_table','Com_alter_table','Com_create_procedure','Com_create_trigger','Com_drop_procedure')"
}
schema_commands > "$work/schema-before"
test -s "$work/schema-before"
"$work/system-server" prepare-database system
schema_commands > "$work/schema-after"
diff -u "$work/schema-before" "$work/schema-after"
test "$(echo "SELECT COUNT(*) FROM oauth_clients WHERE client_type='confidential' AND status='disabled' AND client_secret_hash IS NULL" | admin)" = 54
test "$(echo "SELECT COUNT(*) FROM oauth_clients WHERE client_id='addp-cli' AND status='active' AND client_type='public' AND service_principal_id IS NULL" | admin)" = 1
test "$(echo "SELECT COUNT(*) FROM role_assignments WHERE scope_type='platform' AND source_type='bootstrap'" | admin)" = 53
test "$(echo "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND definer='iam_installer@%'" | admin)" = "$expected"
admin <<'SQL'
SQL
test "$(echo "SELECT version>0 AND pending_version=0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | runtime)" = 1
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner IN ('system_iam_foundation','system_access_request_locks','system_module_registry','system_engine_catalog','system_access_target_locks','system_source_grant_catalog','system_engine_delegations','system_cleanup_requests','system_execution_authorization','system_search_authorization') AND version>0 AND pending_version=0" | runtime)" = 10
runtime <<'SQL'
INSERT INTO cleanup_requests(request_id,tenant_id,requested_by,parent_execution_id,intent_digest,source_operation_id,action,protocol,engine_confirmation,requested_scope)
VALUES ('00000000-0000-4000-8000-000000000001',1,1,'00000000-0000-4000-8000-000000000002',SHA2('scan',256),'source-1','scan','{ "signed": true }','','[]');
SQL
test "$(runtime <<<"SELECT protocol FROM cleanup_requests")" = '{ "signed": true }'
if runtime <<<"UPDATE cleanup_requests SET requested_by=2" 2>"$work/cleanup.err"; then exit 1; fi
grep -q 'cleanup acceptance and cutoff are immutable' "$work/cleanup.err"
if admin <<<"UPDATE cleanup_requests SET requested_by=2,cutoff='{}'" 2>"$work/cleanup.err"; then exit 1; fi
grep -q 'cleanup acceptance and cutoff are immutable' "$work/cleanup.err"
runtime <<<"UPDATE cleanup_requests SET cutoff='{}'"
if runtime <<<"UPDATE cleanup_requests SET cutoff='{}'" 2>"$work/cleanup.err"; then exit 1; fi
grep -q 'cleanup acceptance and cutoff are immutable' "$work/cleanup.err"
test "$(echo "SELECT revision FROM module_registry_state WHERE id=1" | admin)" = 1
echo "INSERT INTO audit_logs(event_name,result,risk_level,module_name) VALUES ('iam.fixture.install','succeeded','low','system')" | runtime
for denied in 'TRUNCATE TABLE audit_logs' 'ALTER TABLE audit_logs ADD COLUMN fixture INT'; do
    if echo "$denied" | runtime 2>"$work/denied.err"; then exit 1; fi
    grep -q 'ERROR 1142' "$work/denied.err"
done
for rewrite in "UPDATE audit_logs SET result='failed'" 'DELETE FROM audit_logs'; do
    if echo "$rewrite" | runtime 2>"$work/denied.err"; then exit 1; fi
    grep -q 'audit logs are append-only' "$work/denied.err"
done
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.fixture.install' AND result='succeeded'" | admin)" = 1
export IAM_MFA_ENCRYPTION_KEY=$(openssl rand -base64 32)
python3 "$(dirname "$0")/../../system/backend/cmd/iam-bootstrap/testdata/mysql_cli.py" "$work"
test "$(echo "SELECT COUNT(*) FROM iam_bootstrap_state WHERE status='completed' AND secret_hash IS NULL AND completed_at IS NOT NULL" | admin)" = 1
test "$(echo "SELECT COUNT(DISTINCT u.id),COUNT(DISTINCT r.role_key) FROM users u JOIN mfa_credentials m ON m.user_id=u.id AND m.status='active' JOIN role_assignments a ON a.principal_id=u.id AND a.status='active' AND a.scope_type='platform' JOIN roles r ON r.id=a.role_id WHERE r.role_key IN ('platform.system_administrator','platform.security_administrator','platform.audit_administrator')" | admin)" = $'3\t3'
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.bootstrap.administrator_created' AND result='succeeded'" | admin)" = 3
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.bootstrap.completed' AND result='succeeded'" | admin)" = 1
test "$(echo "SELECT COUNT(*) FROM role_conflicts WHERE reason='platform_three_administrators_separation_of_duties'" | admin)" = 3
admin <<SQL
CREATE DATABASE common;
GRANT ALL ON common.* TO 'iam_installer'@'%' WITH GRANT OPTION;
GRANT SELECT,INSERT,UPDATE,DELETE ON common.* TO 'iam_runtime'@'%';
SQL
"$work/system-server" prepare-database common
sql_dir="$(cd "$(dirname "$0")/../../system/backend/internal/migration/mysql_source_grant" && pwd)"
export ADDP_DATABASE_RUNTIME_USER=iam_runtime ADDP_DATABASE_RUNTIME_PASSWORD="$runtime_password"
export ADDP_TEST_STARTUP_BINARY="$work/system-server"
test_startup() {
    (cd "$(dirname "$0")/../../common" && ADDP_TEST_STARTUP_REASON="$1" GOWORK=off go test ./infra/bootstrap -run '^TestMySQLRuntimeStartupRefusal$' -count=1 -v)
}
admin <<<"UPDATE startup_schema_revision SET pending_version=version WHERE owner='system_source_grant_procedure'"
test_startup 'unfinished MySQL migration'
admin <<<"UPDATE startup_schema_revision SET pending_version=0 WHERE owner='system_source_grant_procedure'"
admin <<'SQL'
INSERT INTO module_definitions(module_name,route_prefix,enabled,declaration_published_at)
VALUES('system','/system',TRUE,NOW(6));
INSERT INTO common.task_executions (tenant_id,execution_id,module,task_type,status,trigger_type,created_at,updated_at,completed_at)
VALUES (1,'00000000-0000-4000-8000-000000000031','meta','scan','success','manual',NOW(6)-INTERVAL 35 DAY,NOW(6)-INTERVAL 29 DAY,NOW(6)-INTERVAL 29 DAY);
INSERT INTO common.execution_events(execution_id,tenant_id,module,task_type,attempt,occurred_at,kind)
VALUES ('00000000-0000-4000-8000-000000000031',1,'meta','scan',1,NOW(6)-INTERVAL 31 DAY,'started'),
       ('00000000-0000-4000-8000-000000000031',1,'meta','scan',1,NOW(6)-INTERVAL 29 DAY,'completed');
SQL

test "$(runtime <<<"SELECT CONCAT(version, '/', applied_version) FROM iam_security_policy")" = '1/0'
# Use the production deployment CLI with the same runtime credentials, through stdin only.
publisher_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-gateway-publisher'")
initial_version=$(admin <<<"SELECT authorization_version FROM principals WHERE id=$publisher_id")
publisher_secret=$(openssl rand -hex 24)
provision_publisher() {
    printf '{"secret":"%s"}' "$publisher_secret" | "$work/system-server" provision-module-publisher gateway
}
provision_publisher
provision_publisher
publisher_secret=$(openssl rand -hex 24)
provision_publisher
# Reuse this publisher through the production declaration protocol during the login test.
export ADDP_TEST_GATEWAY_PUBLISHER_SECRET="$publisher_secret"
unset publisher_secret
test "$(admin <<<"SELECT authorization_version FROM principals WHERE id=$publisher_id")" = "$((initial_version + 1))"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE entity_id='$publisher_id' AND event_name='iam.service_principal.credential.provisioned'")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE entity_id='$publisher_id' AND event_name='iam.service_principal.credential.rotated'")" = 1
echo 'System publisher CLI: initial credential, idempotent provision and audited rotation verified'
admin <<'SQL'
CREATE DATABASE gateway;
SQL
admin <<'SQL'
GRANT ALL ON gateway.* TO 'iam_installer'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON gateway.* TO 'iam_runtime'@'%';
SQL
export ADDP_TEST_MYSQL_MEMBER_PASSWORD=$(openssl rand -hex 24)
bash "$(dirname "$0")/mysql-gateway-install-contract.sh" "$work" "$container" "$3"
(cd "$(dirname "$0")/../../system/backend" && ADDP_TEST_MYSQL_CLEANUP_ACCEPTANCE=1 GOWORK=off go test ./internal/service -run '^TestCleanupAcceptanceAgainstMySQL$' -count=1 -v) | tee "$work/cleanup-acceptance.log"
grep -q '^--- PASS: TestCleanupAcceptanceAgainstMySQL ' "$work/cleanup-acceptance.log"
expected_settings=$(runtime <<<"SELECT CONCAT('max_connections=',@@GLOBAL.max_connections),CONCAT('time_zone=',@@GLOBAL.time_zone),CONCAT('transaction_isolation=',@@GLOBAL.transaction_isolation)")
(cd "$(dirname "$0")/../../system/backend" && ADDP_TEST_MYSQL_ENGINE_OBSERVATION=1 GOWORK=off go test ./internal/repository -run '^TestEngineAddressObservationAgainstMySQL$' -count=1 -v) | tee "$work/engine-observation.log"
grep -q '^--- PASS: TestEngineAddressObservationAgainstMySQL ' "$work/engine-observation.log"
(cd "$(dirname "$0")/../../system/backend" && ADDP_TEST_MYSQL_GLOBAL_SETTINGS="$expected_settings" GOWORK=off go test ./internal/infraconfig -run '^TestMySQLReadOnlyInstanceConfiguration$' -count=1 -v) | tee "$work/readonly-mysql-settings.log"
grep -q '^--- PASS: TestMySQLReadOnlyInstanceConfiguration ' "$work/readonly-mysql-settings.log"
(cd "$(dirname "$0")/../../system/backend" && ADDP_TEST_MYSQL_TENANT_FACTS="$work/tenant.json" GOWORK=off go test ./internal/migration -run '^TestPrepareMySQLGatewayTenant$' -count=1 -v)
(
    cd "$(dirname "$0")/../../system/backend"
    unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
    ADDP_TEST_MYSQL_ONTOLOGY_AUTHORIZATION=1 GOWORK=off go test ./internal/migration \
        -run '^TestMySQLOntologyProjectionAuthorization$' -count=1 -v
) | tee "$work/ontology-projection-authorization.log"
grep -q '^--- PASS: TestMySQLOntologyProjectionAuthorization ' "$work/ontology-projection-authorization.log"
python3 "$(dirname "$0")/mysql-system-startup.py" "$work"
test "$(runtime <<<"SELECT CONCAT(version, '/', applied_version) FROM iam_security_policy")" = '1/1'
test "$(runtime <<<"SELECT COUNT(*),MIN(kind) FROM common.execution_events WHERE execution_id='00000000-0000-4000-8000-000000000031'")" = $'1\tcompleted'
test "$(runtime <<<"SELECT status FROM common.task_executions WHERE execution_id='00000000-0000-4000-8000-000000000031'")" = success
test "$(admin <<<"SELECT COUNT(*) FROM module_runtime_instances i JOIN module_definitions d ON d.id=i.module_definition_id WHERE d.module_name='system' AND i.role='backend' AND i.status='down' AND i.stop_reason='graceful'")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.service_principal.credential.provisioned'")" -gt 0
test "$(admin <<<"SELECT COUNT(*) FROM module_runtime_instances i JOIN module_definitions d ON d.id=i.module_definition_id WHERE d.module_name='gateway' AND i.role='ingress' AND i.status='down' AND i.stop_reason='graceful'")" = 1
echo 'System startup facts: applied IAM policy, pruned 31-day event, retained 29-day event and execution, audited credentials and closed backend lease'

echo "UPDATE startup_schema_revision SET pending_version=version+1 WHERE owner='system_module_registry'" | admin
test_startup 'schema system_module_registry has unfinished MySQL migration'
