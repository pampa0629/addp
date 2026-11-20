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
result=0
"$work/system-server" install-mysql-iam > "$work/triggers.sql" || result=$?
test "$result" = 3
test "$(echo "SELECT version=0 AND pending_version>0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | admin)" = 1
expected=$(grep -c '^CREATE DEFINER=' "$work/triggers.sql")
test "$expected" -gt 0
# A same-name object with a different body must not authorize startup.
admin <<'SQL'
CREATE DEFINER='iam_installer'@'%' TRIGGER trg_principals_updated_at BEFORE UPDATE ON principals FOR EACH ROW SET NEW.updated_at = OLD.updated_at;
SQL
result=0
"$work/system-server" install-mysql-iam > "$work/refused.sql" 2>"$work/refused.err" || result=$?
test "$result" = 1
grep -q 'trg_principals_updated_at differs' "$work/refused.err"
echo 'DROP TRIGGER trg_principals_updated_at' | admin
admin < "$work/triggers.sql"
"$work/system-server" install-mysql-iam > "$work/remaining.sql"
test ! -s "$work/remaining.sql"
test "$(echo "SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND definer='iam_installer@%'" | admin)" = "$expected"
admin <<'SQL'
GRANT SELECT ON `system`.startup_schema_revision TO 'iam_runtime'@'%';
GRANT SELECT, INSERT ON `system`.audit_logs TO 'iam_runtime'@'%';
SQL
test "$(echo "SELECT version>0 AND pending_version=0 FROM startup_schema_revision WHERE owner='system_iam_foundation'" | runtime)" = 1
echo "INSERT INTO audit_logs(event_name,result,risk_level,module_name) VALUES ('iam.fixture.install','succeeded','low','system')" | runtime
for denied in 'TRUNCATE TABLE audit_logs' 'ALTER TABLE audit_logs ADD COLUMN fixture INT' "UPDATE audit_logs SET result='failed'" 'DELETE FROM audit_logs' 'UPDATE startup_schema_revision SET version=0'; do
    if echo "$denied" | runtime 2>"$work/denied.err"; then
        echo 'unexpected runtime database privilege' >&2
        exit 1
    fi
    grep -q 'ERROR 1142' "$work/denied.err"
done
test "$(echo "SELECT COUNT(*) FROM audit_logs WHERE event_name='iam.fixture.install' AND result='succeeded'" | runtime)" = 1
echo "MySQL System IAM installation: DBA applied $expected scoped triggers; restricted runtime read version and appended audit"
