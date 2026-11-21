#!/usr/bin/env bash
# Exercise the shipped procedure/result DDL with fixture Grant and IAM parents.
set -euo pipefail
container=$1
work=$2
runtime_password=$(openssl rand -hex 24)
worker_password=$(openssl rand -hex 24)
bootstrap_password=$(openssl rand -hex 24)
admin() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB system'
}
sql_dir="$(cd "$(dirname "$0")/../../system/backend/internal/migration/mysql_source_grant" && pwd)"
admin < "$sql_dir/create_definer.sql"
admin <<SQL
CREATE USER 'system_gate'@'%' IDENTIFIED BY '$runtime_password';
CREATE USER 'worker_gate'@'127.0.0.1' IDENTIFIED BY '$worker_password';
CREATE USER 'bootstrap_gate'@'127.0.0.1' IDENTIFIED BY '$bootstrap_password';
SQL
admin <<'SQL'
CREATE TABLE tenants (id BIGINT PRIMARY KEY);
CREATE TABLE principals (id BIGINT PRIMARY KEY);
CREATE TABLE tenant_memberships (id BIGINT PRIMARY KEY);
INSERT INTO tenants VALUES(7);
INSERT INTO principals VALUES(21);
INSERT INTO tenant_memberships VALUES(31);
CREATE TABLE engine_access_grants (
    request_id CHAR(36) PRIMARY KEY, approval_mode VARCHAR(32) NOT NULL,
    tenant_id BIGINT NOT NULL, engine_id BIGINT NOT NULL, catalog_path JSON NOT NULL,
    catalog_request_id CHAR(36), node_recursive BOOLEAN, item_fingerprint CHAR(64), boundary_hash CHAR(64),
    file_composition JSON, scope_boundary JSON, metadata_target JSON,
    recipient_type VARCHAR(32), recipient_id BIGINT, action VARCHAR(32), expiry_mode VARCHAR(32),
    expires_at DATETIME(6), operator_principal_id BIGINT, operator_membership_id BIGINT,
    operator_authorization_version BIGINT, reason LONGTEXT, producer_owner VARCHAR(255),
    production_execution_id CHAR(36), production_result_id CHAR(36), upload_id CHAR(36),
    issuer_principal_id BIGINT, issuer_membership_id BIGINT,
    granted_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE (tenant_id,producer_owner,production_execution_id,production_result_id,request_id,issuer_principal_id,issuer_membership_id),
    UNIQUE (tenant_id,upload_id,item_fingerprint,request_id,issuer_principal_id,issuer_membership_id)
) ENGINE=InnoDB DEFAULT CHARACTER SET ascii COLLATE ascii_bin;
CREATE TABLE sample_audit (id INT PRIMARY KEY);
SQL
admin < "$sql_dir/initial_results.sql"
{ echo 'DELIMITER $$'; cat "$sql_dir/persist_source_grant.sql"; echo '$$'; } | admin
admin < "$sql_dir/authorize_definer.sql"
admin <<'SQL'
GRANT EXECUTE ON PROCEDURE system.persist_source_grant TO 'system_gate'@'%';
GRANT SELECT ON system.engine_access_grants TO 'system_gate'@'%';
GRANT SELECT ON system.engine_access_initial_grant_outcomes TO 'system_gate'@'%';
GRANT SELECT ON system.engine_access_upload_grant_outcomes TO 'system_gate'@'%';
GRANT INSERT ON system.sample_audit TO 'system_gate'@'%','bootstrap_gate'@'127.0.0.1';
SQL
caller() {
    local user=$1
    shift
    local password=$runtime_password
    case "$user" in
        worker_gate) password=$worker_password ;;
        bootstrap_gate) password=$bootstrap_password ;;
        system_gate) ;;
        *) return 2 ;;
    esac
    MYSQL_PWD="$password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -u"$user" -NB "$@"
}

# --force keeps the same connection for the explicit post-error COMMIT or ROLLBACK.
expect_error() {
    local user=$1 code=$2 statements=$3
    caller "$user" --force <<<"$statements" >"$work/boundary.out" 2>"$work/boundary.err" || true
    test "$(grep -c '^ERROR ' "$work/boundary.err")" = 1
    grep -q "^ERROR $code " "$work/boundary.err"
}
test "$(admin <<<"SELECT CONCAT(SECURITY_TYPE, '/', DEFINER) FROM information_schema.routines WHERE routine_schema='system' AND routine_name='persist_source_grant'")" = 'DEFINER/addp_system_source_grant@localhost'
test "$(admin <<<"SELECT account_locked FROM mysql.user WHERE user='addp_system_source_grant' AND host='localhost'")" = Y
# Check the complete grant list, including role assignments and grant options.
check_grants() {
    local account=$1
    shift
    {
        echo "GRANT USAGE ON *.* TO $account"
        for grant in "$@"; do echo "GRANT $grant TO $account"; done
    } | LC_ALL=C sort >"$work/expected-grants"
    admin <<<"SHOW GRANTS FOR '${account%@*}'@'${account#*@}'" | sed 's/`//g' | LC_ALL=C sort >"$work/actual-grants"
    diff -u "$work/expected-grants" "$work/actual-grants"
}
check_grants addp_system_source_grant@localhost 'EXECUTE ON PROCEDURE system.persist_source_grant' \
    'INSERT ON system.engine_access_grants' 'INSERT ON system.engine_access_initial_grant_outcomes' 'INSERT ON system.engine_access_upload_grant_outcomes'
check_grants 'system_gate@%' 'EXECUTE ON PROCEDURE system.persist_source_grant' 'SELECT ON system.engine_access_grants' \
    'SELECT ON system.engine_access_initial_grant_outcomes' 'SELECT ON system.engine_access_upload_grant_outcomes' 'INSERT ON system.sample_audit'
check_grants worker_gate@127.0.0.1
check_grants bootstrap_gate@127.0.0.1 'INSERT ON system.sample_audit'
uuid() { printf "'00000000-0000-4000-8000-%012d'" "$1"; }
# Shared typed inputs keep the same request, issuer, tenant and source keys
# visible across the denial, uniqueness and rollback checks.
call() {
    local mode=$1 request=$2 result=$3 denial=$4
    local owner=NULL execution=NULL result_id=NULL upload=NULL issuer=NULL membership=NULL recorded=NULL catalog_request=NULL
    local fingerprint="SHA2('item',256)"
    case "$mode" in
        task_production) owner="'transfer'"; execution=$(uuid 100); result_id=$(uuid "$result") ;;
        upload_creation) upload=$(uuid "$result") ;;
        catalog) catalog_request=$request ;;
        independent) ;;
        *) return 2 ;;
    esac
    case "$mode" in task_production|upload_creation)
        issuer=21; membership=31; recorded="'2026-01-02 03:04:05.123456'" ;;
    esac
    cat <<SQL
CALL system.persist_source_grant('$mode',$request,7,99,JSON_OBJECT('engine_id',99),$catalog_request,NULL,
    $fingerprint,SHA2('boundary',256),NULL,NULL,NULL,'user',41,'read','until_revoked',NULL,41,51,1,NULL,
    $owner,$execution,$result_id,$upload,$issuer,$membership,$recorded,$denial)
SQL
}
pair=$(call task_production "$(uuid 11)" 1 NULL)
expect_error worker_gate 1370 "START TRANSACTION; $pair; ROLLBACK;"
expect_error bootstrap_gate 1370 "START TRANSACTION; $pair; ROLLBACK;"
expect_error system_gate 1305 "$pair;"
for table in engine_access_grants engine_access_initial_grant_outcomes engine_access_upload_grant_outcomes; do
    expect_error system_gate 1142 "INSERT INTO system.$table () VALUES ();"
done
caller system_gate <<SQL
START TRANSACTION;
$pair;
$(call upload_creation "$(uuid 12)" 2 NULL);
$(call task_production NULL 3 "'source_access_denied'");
$(call upload_creation NULL 4 "'not_new'");
$(call catalog "$(uuid 13)" 0 NULL);
$(call independent "$(uuid 14)" 0 NULL);
INSERT INTO system.sample_audit VALUES(1),(2),(3),(4),(5),(6);
COMMIT;
SQL
expect_error system_gate 1062 "START TRANSACTION; INSERT INTO system.sample_audit VALUES(7);
 $(call task_production "$(uuid 15)" 1 NULL); COMMIT;"
expect_error system_gate 1062 "START TRANSACTION; $(call upload_creation "$(uuid 16)" 6 NULL);
 INSERT INTO system.sample_audit VALUES(1); ROLLBACK;"
expect_error system_gate 1644 "START TRANSACTION; $(call independent "$(uuid 17)" 0 "'not_new'"); COMMIT;"
expect_error system_gate 3819 "START TRANSACTION; $(call task_production NULL 7 "'not_new'"); ROLLBACK;"
for table in engine_access_initial_grant_outcomes engine_access_upload_grant_outcomes; do
    test "$(admin <<<"SELECT COUNT(*), SUM(outcome='issued'), SUM(outcome='denied') FROM $table")" = $'2\t1\t1'
done
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants")" = 4
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE approval_mode IN ('catalog','independent') AND request_id IN ($(uuid 13),$(uuid 14))")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_initial_grant_outcomes WHERE grant_request_id=$(uuid 11) AND result_id=$(uuid 1) AND issuer_principal_id=21 AND issuer_membership_id=31 AND recorded_at='2026-01-02 03:04:05.123456'")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_upload_grant_outcomes WHERE grant_request_id=$(uuid 12) AND upload_id=$(uuid 2) AND item_fingerprint=SHA2('item',256)")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM sample_audit")" = 7
echo 'MySQL System source procedure: 4 Grants, 2 issued and 2 denied across both result tables; privileges and rollback verified'
(cd "$sql_dir/../../.." && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
    go test ./internal/engineaccess -run '^TestSourceGrantWriterAgainstMySQL$' -count=1 -timeout=60s -v) | tee "$work/source-writer.log"
grep -q '^--- PASS: TestSourceGrantWriterAgainstMySQL ' "$work/source-writer.log"
