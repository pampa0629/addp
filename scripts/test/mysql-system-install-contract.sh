#!/usr/bin/env bash
# Called only by the gate that owns this disposable MySQL container.
set -euo pipefail
work=$1
container=$2
export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3" ADDP_INFRA_DATABASE_PROVIDER=mysql
export ADDP_MYSQL_INSTALL_USER=iam_installer
export ADDP_MYSQL_INSTALL_PASSWORD=$(openssl rand -hex 24)
runtime_password=$(openssl rand -hex 24)
admin() {
    docker exec -i "$container" sh -c 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB system'
}
runtime() {
    MYSQL_PWD="$runtime_password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -uiam_runtime -NB system
}
admin <<SQL
CREATE USER 'iam_installer'@'%' IDENTIFIED BY '$ADDP_MYSQL_INSTALL_PASSWORD';
CREATE USER 'iam_runtime'@'%' IDENTIFIED BY '$runtime_password';
GRANT SELECT, INSERT, UPDATE, CREATE, REFERENCES, INDEX, ALTER, TRIGGER, CREATE ROUTINE, EXECUTE ON \`system\`.* TO 'iam_installer'@'%';
SQL
test "$(echo 'SELECT @@log_bin, @@log_bin_trust_function_creators' | admin)" = $'1\t0'
# An unknown argument must not launch the HTTP server or perform installation.
result=0
"$work/system-server" install-mysql-iam > "$work/retired.out" 2> "$work/retired.err" || result=$?
test "$result" = 2
test ! -s "$work/retired.out"
result=0
"$work/system-server" install-mysql-system > "$work/triggers.sql" || result=$?
test "$result" = 3
test "$(echo "SELECT version=0 AND pending_version>0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | admin)" = 1
expected=$(grep -c '^CREATE DEFINER=' "$work/triggers.sql")
test "$expected" -gt 0
for name in module_node_declaration_immutable process_metrics_declaration_immutable; do
    grep -q "TRIGGER IF NOT EXISTS $name$" "$work/triggers.sql"
done
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner IN ('system_iam_foundation','system_module_registry') AND version=0 AND pending_version>0" | admin)" = 2
test "$(echo "SELECT version=1 AND pending_version=0 FROM startup_schema_revision WHERE owner='system_access_request_locks'" | admin)" = 1
# A same-name object with a different body must not authorize startup.
admin <<'SQL'
CREATE DEFINER='iam_installer'@'%' TRIGGER trg_principals_updated_at BEFORE UPDATE ON principals FOR EACH ROW SET NEW.updated_at = OLD.updated_at;
SQL
result=0
"$work/system-server" install-mysql-system > "$work/refused.sql" 2>"$work/refused.err" || result=$?
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
"$work/system-server" install-mysql-system > "$work/incomplete.out" 2>"$work/incomplete.err" || result=$?
test "$result" = 1
grep -q 'fixture rejected revision completion' "$work/incomplete.err"
test "$(echo 'SELECT (SELECT COUNT(*) FROM principals)+(SELECT COUNT(*) FROM oauth_clients)+(SELECT COUNT(*) FROM roles)+(SELECT COUNT(*) FROM permissions)+(SELECT COUNT(*) FROM role_assignments)+(SELECT COUNT(*) FROM role_permissions)' | admin)" = 0
test "$(echo "SELECT version=0 AND pending_version>0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | admin)" = 1
echo 'DROP TRIGGER fixture_refuse_completion' | admin
# The DBA may apply a partial list. Completed owners remain intact on replay.
echo 'DROP TRIGGER process_metrics_declaration_immutable' | admin
result=0
"$work/system-server" install-mysql-system > "$work/last-trigger.sql" || result=$?
test "$result" = 3
test "$(grep -c '^CREATE DEFINER=' "$work/last-trigger.sql")" = 1
grep -q 'TRIGGER IF NOT EXISTS process_metrics_declaration_immutable$' "$work/last-trigger.sql"
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner IN ('system_access_request_locks','system_iam_foundation') AND version>0 AND pending_version=0" | admin)" = 2
test "$(echo 'SELECT COUNT(*) FROM module_registry_state' | admin)" = 0
admin < "$work/last-trigger.sql"
"$work/system-server" install-mysql-system > "$work/remaining.sql"
test ! -s "$work/remaining.sql"
"$work/system-server" install-mysql-system
test "$(echo "SELECT COUNT(*) FROM oauth_clients WHERE client_type='confidential' AND status='disabled' AND client_secret_hash IS NULL" | admin)" = 54
test "$(echo "SELECT COUNT(*) FROM oauth_clients WHERE client_id='addp-cli' AND status='active' AND client_type='public' AND service_principal_id IS NULL" | admin)" = 1
test "$(echo "SELECT COUNT(*) FROM role_assignments WHERE scope_type='platform' AND source_type='bootstrap'" | admin)" = 53
test "$(echo "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND definer='iam_installer@%'" | admin)" = "$expected"
admin <<'SQL'
GRANT SELECT ON `system`.startup_schema_revision TO 'iam_runtime'@'%';
GRANT SELECT, INSERT ON `system`.audit_logs TO 'iam_runtime'@'%';
SQL
test "$(echo "SELECT version>0 AND pending_version=0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | runtime)" = 1
test "$(echo "SELECT COUNT(*) FROM startup_schema_revision WHERE owner IN ('system_iam_foundation','system_access_request_locks','system_module_registry','system_engine_catalog','system_access_target_locks','system_source_grant_catalog') AND version>0 AND pending_version=0" | runtime)" = 6
test "$(echo "SELECT revision FROM module_registry_state WHERE id=1" | admin)" = 1
echo "INSERT INTO audit_logs(event_name,result,risk_level,module_name) VALUES ('iam.fixture.install','succeeded','low','system')" | runtime
for denied in 'TRUNCATE TABLE audit_logs' 'ALTER TABLE audit_logs ADD COLUMN fixture INT' "UPDATE audit_logs SET result='failed'" 'DELETE FROM audit_logs' 'UPDATE startup_schema_revision SET version=0'; do
    if echo "$denied" | runtime 2>"$work/denied.err"; then
        echo 'unexpected runtime database privilege' >&2
        exit 1
    fi
    grep -q 'ERROR 1142' "$work/denied.err"
done
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.fixture.install' AND result='succeeded'" | runtime)" = 1
export ADDP_MYSQL_BOOTSTRAP_USER=iam_bootstrap_gate
export ADDP_MYSQL_BOOTSTRAP_PASSWORD=$(openssl rand -hex 24)
export IAM_MFA_ENCRYPTION_KEY=$(openssl rand -base64 32)
admin <<SQL
CREATE USER 'iam_bootstrap_gate'@'%' IDENTIFIED BY '$ADDP_MYSQL_BOOTSTRAP_PASSWORD';
GRANT SELECT ON system.startup_schema_revision TO 'iam_bootstrap_gate'@'%';
GRANT SELECT,INSERT,UPDATE ON system.iam_bootstrap_state TO 'iam_bootstrap_gate'@'%';
GRANT SELECT,INSERT ON system.principals TO 'iam_bootstrap_gate'@'%';
GRANT SELECT ON system.roles TO 'iam_bootstrap_gate'@'%';
GRANT INSERT ON system.role_assignments TO 'iam_bootstrap_gate'@'%';
GRANT INSERT ON system.users TO 'iam_bootstrap_gate'@'%';
GRANT INSERT ON system.local_accounts TO 'iam_bootstrap_gate'@'%';
GRANT INSERT ON system.mfa_credentials TO 'iam_bootstrap_gate'@'%';
GRANT INSERT ON system.audit_logs TO 'iam_bootstrap_gate'@'%';
SQL
# Run with the temporary credential alone. Keep the TTY and all secrets in memory.
python3 "$(dirname "$0")/../../system/backend/cmd/iam-bootstrap/testdata/mysql_cli.py" "$work"
test "$(echo "SELECT COUNT(*) FROM iam_bootstrap_state WHERE status='completed' AND secret_hash IS NULL AND completed_at IS NOT NULL" | admin)" = 1
test "$(echo "SELECT COUNT(DISTINCT u.id),COUNT(DISTINCT r.role_key) FROM users u JOIN mfa_credentials m ON m.user_id=u.id AND m.status='active' JOIN role_assignments a ON a.principal_id=u.id AND a.status='active' AND a.scope_type='platform' JOIN roles r ON r.id=a.role_id WHERE r.role_key IN ('platform.system_administrator','platform.security_administrator','platform.audit_administrator')" | admin)" = $'3\t3'
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.bootstrap.administrator_created' AND result='succeeded'" | admin)" = 3
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.bootstrap.completed' AND result='succeeded'" | admin)" = 1
test "$(echo "SELECT COUNT(*) FROM role_conflicts WHERE reason='platform_three_administrators_separation_of_duties'" | admin)" = 3
if echo 'SELECT secret_hash FROM iam_bootstrap_state' | runtime 2>"$work/state-denied.err"; then
    echo 'runtime account can read the offline bootstrap state' >&2
    exit 1
fi
grep -q 'ERROR 1142' "$work/state-denied.err"
echo "DROP USER 'iam_bootstrap_gate'@'%'" | admin
if MYSQL_PWD="$ADDP_MYSQL_BOOTSTRAP_PASSWORD" docker exec -e MYSQL_PWD "$container" mysql -h127.0.0.1 -uiam_bootstrap_gate -e 'SELECT 1' 2>"$work/closed.err"; then
    echo 'temporary bootstrap account still authenticates' >&2
    exit 1
fi
grep -q 'ERROR 1045' "$work/closed.err"
echo 'MySQL offline CLI: three distinct administrators, MFA, audit and temporary account removal verified'
echo "MySQL System installation: DBA applied $expected scoped triggers; restricted runtime read version and appended audit"

# Supply only the runtime account to the normal System process. Install Common
# with its separate owner so all published revisions are genuine.
common_password=$(openssl rand -hex 24)
admin <<SQL
CREATE DATABASE common;
CREATE USER 'common_setup'@'%' IDENTIFIED BY '$common_password';
GRANT SELECT,INSERT,UPDATE,CREATE,INDEX,ALTER,REFERENCES ON common.* TO 'common_setup'@'%';
SQL
ADDP_MYSQL_INSTALL_USER=common_setup ADDP_MYSQL_INSTALL_PASSWORD="$common_password" "$work/system-server" install-mysql-common
echo "GRANT SELECT ON common.startup_schema_revision TO 'iam_runtime'@'%'" | admin
export ADDP_MYSQL_SYSTEM_USER=iam_runtime ADDP_MYSQL_SYSTEM_PASSWORD="$runtime_password"
export ADDP_TEST_STARTUP_BINARY="$work/system-server"
test_startup() {
    (cd "$(dirname "$0")/../../common" && ADDP_TEST_STARTUP_REASON="$1" GOWORK=off go test ./infra/bootstrap -run '^TestMySQLRuntimeStartupRefusal$' -count=1 -v)
}
test_startup 'System MySQL installation does not yet include complete source authorization guards'
echo "UPDATE startup_schema_revision SET pending_version=version+1 WHERE owner='system_module_registry'" | admin
test_startup 'schema system_module_registry has unfinished MySQL migration'
