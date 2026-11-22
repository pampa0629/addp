#!/usr/bin/env bash
# Exercise the shipped procedure against tables and provenance guards from the System installer.
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
export ADDP_INFRA_DATABASE_PROVIDER=mysql ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$ADDP_TEST_MYSQL_PORT"
export ADDP_MYSQL_INSTALL_USER=source_installer
ADDP_MYSQL_INSTALL_PASSWORD=$(openssl rand -hex 24)
export ADDP_MYSQL_INSTALL_PASSWORD
admin <<SQL
CREATE USER 'source_installer'@'%' IDENTIFIED BY '$ADDP_MYSQL_INSTALL_PASSWORD';
SQL
admin <<'SQL'
GRANT SELECT, INSERT, UPDATE, CREATE, REFERENCES, INDEX, ALTER, TRIGGER, CREATE ROUTINE, EXECUTE ON `system`.* TO 'source_installer'@'%';
SQL
result=0
"$work/system-server" install-mysql-system > "$work/source-triggers.sql" || result=$?
test "$result" = 3
admin < "$work/source-triggers.sql"
"$work/system-server" install-mysql-system > "$work/source-replay.sql"
test ! -s "$work/source-replay.sql"
admin < "$sql_dir/create_definer.sql"
admin <<SQL
CREATE USER 'system_gate'@'%' IDENTIFIED BY '$runtime_password';
CREATE USER 'worker_gate'@'127.0.0.1' IDENTIFIED BY '$worker_password';
CREATE USER 'bootstrap_gate'@'127.0.0.1' IDENTIFIED BY '$bootstrap_password';
SQL
admin <<'SQL'
INSERT INTO tenants(id,code,name) VALUES(7,'grant-source','Grant source');
INSERT INTO principals(id,principal_type) VALUES(40001,'user');
INSERT INTO users(id,display_name) VALUES(40001,'Source writer fixture');
INSERT INTO tenant_memberships(id,tenant_id,principal_id,source_type,joined_at)
VALUES(50001,7,40001,'manual',NOW(6));
UPDATE oauth_clients SET status='active',client_secret_hash='isolated-unusable-test-hash'
WHERE client_id IN ('addp-transfer','addp-manager');
INSERT INTO tenant_memberships(id,tenant_id,principal_id,source_type,joined_at)
SELECT IF(client_id='addp-transfer',31001,31002),7,service_principal_id,'manual',NOW(6) FROM oauth_clients
WHERE client_id IN ('addp-transfer','addp-manager');
INSERT INTO engines(id,tenant_id,name,engine_type,connection_info,identity_key)
VALUES (12,7,'Grant records','postgresql',JSON_OBJECT(),JSON_OBJECT('id',12)),
       (99,7,'Grant files','nfs',JSON_OBJECT(),JSON_OBJECT('id',99));
CREATE TABLE sample_audit (id INT PRIMARY KEY);
SQL
transfer_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-transfer'")
manager_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-manager'")
catalog_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-catalog'")
human_version=$(admin <<<"SELECT authorization_version FROM principals WHERE id=40001")
{ echo 'DELIMITER $$'; cat "$sql_dir/persist_source_grant.sql"; echo '$$'; } | admin
admin < "$sql_dir/authorize_definer.sql"
admin <<'SQL'
GRANT EXECUTE ON PROCEDURE system.persist_source_grant TO 'system_gate'@'%';
GRANT SELECT, INSERT ON system.engine_access_grant_revocations TO 'system_gate'@'%';
GRANT SELECT, INSERT ON system.engine_access_fulfillment_outcomes TO 'system_gate'@'%';
GRANT SELECT, INSERT, UPDATE ON system.engine_access_request_locks TO 'system_gate'@'%';
GRANT INSERT (target_key), UPDATE (lock_marker) ON system.engine_access_target_locks TO 'system_gate'@'%';
GRANT INSERT ON system.audit_logs TO 'system_gate'@'%';
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
    'SELECT, INSERT ON system.engine_access_grant_revocations' 'SELECT, INSERT ON system.engine_access_fulfillment_outcomes' 'SELECT, INSERT, UPDATE ON system.engine_access_request_locks' \
    'INSERT (target_key), UPDATE (lock_marker) ON system.engine_access_target_locks' 'INSERT ON system.audit_logs' \
    'SELECT ON system.engine_access_initial_grant_outcomes' 'SELECT ON system.engine_access_upload_grant_outcomes' 'INSERT ON system.sample_audit'
check_grants worker_gate@127.0.0.1
check_grants bootstrap_gate@127.0.0.1 'INSERT ON system.sample_audit'
uuid() { printf "'00000000-0000-4000-8000-%012d'" "$1"; }
# Shared typed inputs keep the same request, issuer, tenant and source keys
# visible across the denial, uniqueness and rollback checks.
call() {
    local mode=$1 request=$2 result=$3 denial=$4
    local owner=NULL execution=NULL result_id=NULL upload=NULL issuer=NULL membership=NULL recorded=NULL catalog_request=NULL
    local engine=12 path="$table_path" fingerprint="SHA2('12:public.orders',256)" reason=NULL composition=NULL
    case "$mode" in
        task_production) owner="'transfer'"; execution=$(uuid 100); result_id=$(uuid "$result"); issuer=$transfer_id; membership=31001
            path="JSON_SET($table_path,'$.segments[2].name','production_$result')"
            fingerprint="SHA2('12:public.production_$result',256)" ;;
        upload_creation)
            engine=99; path="JSON_SET($file_path,'$.segments[1].name','upload_$result.geojson')"; fingerprint="SHA2('99:upload_$result.geojson',256)"; upload=$(uuid "$result")
            issuer=$manager_id; membership=31002
            composition="JSON_OBJECT('format','geojson','refs',JSON_ARRAY(JSON_OBJECT('target',$path,'role','data','primary',true,'required',true)))" ;;
        catalog) catalog_request=$request ;;
        independent) reason="'fixture approval'"
            path="JSON_SET($table_path,'$.segments[2].name','independent_orders')"
            fingerprint="SHA2('12:public.independent_orders',256)" ;;
        *) return 2 ;;
    esac
    case "$mode" in task_production|upload_creation)
        recorded="'2026-01-02 03:04:05.123456'" ;;
    esac
    cat <<SQL
CALL system.persist_source_grant('$mode',$request,7,$engine,$path,$catalog_request,NULL,
    $fingerprint,SHA2('boundary',256),$composition,NULL,NULL,'user',40001,'read','until_revoked',NULL,40001,50001,$human_version,$reason,
    $owner,$execution,$result_id,$upload,$issuer,$membership,$recorded,$denial)
SQL
}
table_path="JSON_OBJECT('version','catalog.path/v1','engine_id',12,'segments',JSON_ARRAY(JSON_OBJECT('term','server','kind','server','name',''),JSON_OBJECT('term','schema','kind','namespace','name','public'),JSON_OBJECT('term','table','kind','table','name','orders')))"
file_path="JSON_OBJECT('version','catalog.path/v1','engine_id',99,'segments',JSON_ARRAY(JSON_OBJECT('term','root','kind','root','name',''),JSON_OBJECT('term','file','kind','file','name','upload.geojson')))"
caller system_gate system <<SQL
SET @recorded=NOW(6);
INSERT INTO engine_access_fulfillment_outcomes (request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,outcome,recorded_at,deadline)
VALUES ($(uuid 13),7,12,$catalog_id,$table_path,
  JSON_OBJECT('decision_id',$(uuid 113),'expiry_mode','until_revoked','expires_at',NULL,'item_fingerprint',SHA2('12:public.orders',256),'boundary_hash',SHA2('receipt',256),
    'recipient_type','user','recipient_id',40001,'action','read',
    'operator',JSON_OBJECT('principal_id','40001','tenant_membership_id','50001','authorization_version','$human_version')),
  'until_revoked','accepted',@recorded,@recorded+INTERVAL 5 MINUTE);
SQL
receipt=system.engine_access_fulfillment_outcomes
test "$(caller system_gate <<<"SELECT outcome FROM $receipt WHERE request_id=$(uuid 13)")" = accepted
for user in worker_gate bootstrap_gate; do
    expect_error "$user" 1142 "SELECT outcome FROM $receipt;"
    expect_error "$user" 1142 "INSERT INTO $receipt () VALUES ();"
done
for statement in "UPDATE $receipt SET outcome='closed' WHERE request_id=$(uuid 13)" "DELETE FROM $receipt WHERE request_id=$(uuid 13)"; do
    expect_error system_gate 1142 "$statement"
    # The table's immutable triggers must reject even an account with DML privileges.
    if admin <<<"$statement" >"$work/immutable.out" 2>"$work/immutable.err"; then exit 1; fi
    grep -q '^ERROR 1644 ' "$work/immutable.err"
done
expect_error system_gate 1142 "TRUNCATE TABLE $receipt;"
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
# Result 3 was denied and has no parent Grant. This hits the outcome key
# after the new parent insert, so a following COMMIT exercises the savepoint.
expect_error system_gate 1062 "START TRANSACTION; INSERT INTO system.sample_audit VALUES(7);
 $(call task_production "$(uuid 15)" 3 NULL); COMMIT;"
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE request_id=$(uuid 15)")" = 0
expect_error system_gate 1062 "START TRANSACTION; $(call upload_creation "$(uuid 16)" 6 NULL);
 INSERT INTO system.sample_audit VALUES(1); ROLLBACK;"
expect_error system_gate 1644 "START TRANSACTION; $(call independent "$(uuid 17)" 0 "'not_new'"); COMMIT;"
expect_error system_gate 3819 "START TRANSACTION; $(call task_production NULL 7 "'not_new'"); ROLLBACK;"
# Repeat with a missing Catalog receipt, stale human, wrong service identity,
# and a non-NFS upload. All attempts use the same restricted procedure.
expect_error system_gate 1644 "START TRANSACTION; $(call catalog "$(uuid 81)" 0 NULL); ROLLBACK;"
expect_error system_gate 1644 "START TRANSACTION; $(human_version=0 call independent "$(uuid 82)" 0 NULL); ROLLBACK;"
expect_error system_gate 1644 "START TRANSACTION; $(transfer_id=$manager_id call task_production "$(uuid 83)" 83 NULL); ROLLBACK;"
admin <<<"UPDATE engines SET lifecycle_state='disabled' WHERE id=99"
expect_error system_gate 1644 "START TRANSACTION; $(call upload_creation "$(uuid 84)" 84 NULL); ROLLBACK;"
admin <<<"UPDATE engines SET lifecycle_state='active' WHERE id=99"
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE request_id=$(uuid 13) AND boundary_hash=SHA2('receipt',256)")" = 1
for table in engine_access_initial_grant_outcomes engine_access_upload_grant_outcomes; do
    test "$(admin <<<"SELECT COUNT(*), SUM(outcome='issued'), SUM(outcome='denied') FROM $table")" = $'2\t1\t1'
done
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants")" = 4
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE approval_mode IN ('catalog','independent') AND request_id IN ($(uuid 13),$(uuid 14))")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_initial_grant_outcomes WHERE grant_request_id=$(uuid 11) AND result_id=$(uuid 1) AND issuer_principal_id=$transfer_id AND issuer_membership_id=31001 AND recorded_at='2026-01-02 03:04:05.123456'")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_upload_grant_outcomes WHERE grant_request_id=$(uuid 12) AND upload_id=$(uuid 2) AND item_fingerprint=SHA2('99:upload_2.geojson',256)")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM sample_audit")" = 7
echo 'MySQL System source procedure: 4 Grants, 2 issued and 2 denied across both result tables; privileges and rollback verified'
(cd "$sql_dir/../../.." && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_CATALOG_PRINCIPAL_ID="$catalog_id" ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
    go test ./internal/engineaccess -run '^Test(SourceGrantWriter|FulfillmentReceipt)AgainstMySQL$' -count=1 -timeout=60s -v) | tee "$work/source-writer.log"
grep -q '^--- PASS: TestSourceGrantWriterAgainstMySQL ' "$work/source-writer.log"
grep -q '^--- PASS: TestFulfillmentReceiptAgainstMySQL ' "$work/source-writer.log"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE event_name='system.engine_access_fulfillment.closed'")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_fulfillment_outcomes WHERE outcome='closed'")" = 1
closed_id=$(admin <<<"SELECT request_id FROM engine_access_fulfillment_outcomes WHERE outcome='closed'")
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE event_name='system.engine_access_fulfillment.closed' AND entity_id='$closed_id' AND tenant_id=7 AND principal_id=$catalog_id")" = 1
expect_error system_gate 1644 "START TRANSACTION; $(call catalog "'$closed_id'" 0 NULL); ROLLBACK;"
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE request_id='$closed_id'")" = 0
echo 'System receipt: accepted authorizes the matching Catalog Grant; closed is immutable, audited once and cannot issue a Grant'

# Existing IAM and engine rights for the System service fixture. Only the
# delegation table receives the new write privileges in this installation.
admin <<'SQL'
INSERT INTO principals(id,principal_type) VALUES(40002,'user');
INSERT INTO users(id,display_name) VALUES(40002,'Delegated engine manager');
INSERT INTO tenant_memberships(id,tenant_id,principal_id,source_type,joined_at)
VALUES(50002,7,40002,'manual',NOW(6));
SET @member_version=(SELECT authorization_version FROM principals WHERE id=40002);
INSERT INTO refresh_token_families(principal_id,context_type,tenant_membership_id,issued_authorization_version,client_id,
    auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
VALUES(40002,'tenant',50002,@member_version,'addp-web','first_party',JSON_ARRAY('addp.api'),JSON_ARRAY(),JSON_ARRAY('pwd'),'aal1',
    NOW(6),NOW(6)+INTERVAL 1 HOUR);
SET @role_id=(SELECT id FROM roles WHERE role_key='tenant.engine_access_delegation_administrator');
INSERT INTO role_assignments(principal_id,role_id,scope_type,tenant_id,source_type,created_by_principal_id,grant_reason)
VALUES(40001,@role_id,'tenant',7,'manual',40001,'isolated management fixture');
GRANT SELECT, INSERT, UPDATE (status,version,revoked_by_principal_id,revoked_at,revoked_reason) ON system.engine_access_delegations TO 'system_gate'@'%';
GRANT SELECT, UPDATE (authorization_version,updated_at) ON system.principals TO 'system_gate'@'%';
GRANT SELECT ON system.users TO 'system_gate'@'%';
GRANT SELECT ON system.tenants TO 'system_gate'@'%';
GRANT SELECT, UPDATE (status) ON system.tenant_memberships TO 'system_gate'@'%';
GRANT SELECT, UPDATE (lifecycle_state) ON system.engines TO 'system_gate'@'%';
GRANT SELECT ON system.roles TO 'system_gate'@'%';
GRANT SELECT ON system.role_assignments TO 'system_gate'@'%';
GRANT SELECT ON system.permissions TO 'system_gate'@'%';
GRANT SELECT ON system.role_permissions TO 'system_gate'@'%';
GRANT SELECT, UPDATE (revoked_at,revoked_reason) ON system.refresh_token_families TO 'system_gate'@'%';
SQL
(cd "$sql_dir/../../.." && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
    go test ./internal/engineaccess -run '^TestEngineDelegationAgainstMySQL$' -count=1 -timeout=60s -v) | tee "$work/delegation.log"
grep -q '^--- PASS: TestEngineDelegationAgainstMySQL ' "$work/delegation.log"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE entity_type='engine_access_delegation' AND tenant_id=7 AND principal_id=40001 AND event_name IN ('system.engine_access_delegation.created','system.engine_access_delegation.revoked')")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM refresh_token_families f JOIN principals p ON p.id=f.principal_id WHERE f.principal_id=40002 AND f.revoked_at IS NOT NULL AND f.revoked_reason='engine_access_delegation_changed' AND p.authorization_version=f.issued_authorization_version+2")" = 1
delegations=system.engine_access_delegations
expect_error system_gate 1143 "UPDATE $delegations SET grant_reason='changed' WHERE id=1;"
expect_error system_gate 1142 "DELETE FROM $delegations WHERE id=1;"
expect_error system_gate 1142 "TRUNCATE TABLE $delegations;"
for user in worker_gate bootstrap_gate; do
    expect_error "$user" 1142 "SELECT status FROM $delegations;"
done
test "$(admin <<<"SELECT COUNT(*) FROM information_schema.column_privileges WHERE grantee=CONCAT(QUOTE('system_gate'),'@',QUOTE('%')) AND table_schema='system' AND table_name='engine_access_delegations' AND privilege_type='UPDATE' AND column_name IN ('status','version','revoked_by_principal_id','revoked_at','revoked_reason')")" = 5
echo 'System delegation: restricted create, read and versioned revocation passed; overlap serialized and history preserved'

# An ordinary delegated operator, a separate business confirmer and the
# registered Catalog runtime exercise System's normal application service.
admin <<'SQL'
INSERT INTO principals(id,principal_type) VALUES(40003,'user');
INSERT INTO users(id,display_name) VALUES(40003,'Catalog fulfillment operator');
INSERT INTO tenant_memberships(id,tenant_id,principal_id,source_type,joined_at)
VALUES(50003,7,40003,'manual',NOW(6));
UPDATE oauth_clients SET status='active',client_secret_hash='isolated-unusable-test-hash' WHERE client_id='addp-catalog';
SET @catalog=(SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-catalog');
INSERT INTO tenant_memberships(id,tenant_id,principal_id,source_type,joined_at)
VALUES(50004,7,@catalog,'manual',NOW(6));
INSERT INTO roles(tenant_id,role_key,name,role_type,allowed_scope_types,allowed_principal_types,immutable,created_by_principal_id)
VALUES(7,'custom.catalog_confirmer','Catalog business confirmer','tenant_custom',JSON_ARRAY('tenant'),JSON_ARRAY('user'),0,40001);
SET @confirmer=LAST_INSERT_ID();
SET @entry=(SELECT id FROM permissions WHERE permission_key='catalog.entry.read');
SET @decision=(SELECT id FROM permissions WHERE permission_key='catalog.sharing_decision.create');
INSERT INTO role_permissions(role_id,permission_id,source_type,created_by_principal_id)
VALUES(@confirmer,@entry,'tenant',40001),(@confirmer,@decision,'tenant',40001);
SET @authority=(SELECT id FROM roles WHERE role_key='tenant.source_data_authorizer');
SET @runtime=(SELECT id FROM roles WHERE role_key='tenant.catalog_runtime');
INSERT INTO role_assignments(principal_id,role_id,scope_type,tenant_id,source_type,created_by_principal_id,grant_reason)
VALUES(40002,@confirmer,'tenant',7,'manual',40001,'Catalog business confirmation'),
      (40003,@authority,'tenant',7,'manual',40001,'Catalog handling'),
      (@catalog,@runtime,'tenant',7,'manual',40001,'Catalog runtime');
GRANT SELECT ON system.oauth_clients TO 'system_gate'@'%';
GRANT SELECT ON system.service_principals TO 'system_gate'@'%';
SQL
(cd "$sql_dir/../../.." && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
    go test ./internal/engineaccess -run '^TestCatalogFulfillmentServiceAgainstMySQL$' -count=1 -timeout=60s -v) | tee "$work/catalog-fulfillment.log"
grep -q '^--- PASS: TestCatalogFulfillmentServiceAgainstMySQL ' "$work/catalog-fulfillment.log"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs a JOIN engine_access_fulfillment_outcomes o ON o.request_id=a.entity_id WHERE a.event_name='system.engine_access_fulfillment.accepted' AND a.principal_id=$catalog_id AND o.outcome='accepted' AND o.binding->>'$.operator.principal_id'='40003'")" = 2
revocations=system.engine_access_grant_revocations
for user in worker_gate bootstrap_gate; do
    expect_error "$user" 1142 "SELECT request_id FROM $revocations;"
    expect_error "$user" 1142 "INSERT INTO $revocations () VALUES ();"
done
for stmt in "UPDATE $revocations SET reason='rewrite'" "DELETE FROM $revocations"; do
    expect_error system_gate 1142 "$stmt"
    if admin <<<"$stmt" >"$work/immutable.out" 2>"$work/immutable.err"; then exit 1; fi
    grep -q '^ERROR 1644 ' "$work/immutable.err"
done
expect_error system_gate 1142 "TRUNCATE TABLE $revocations;"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs a JOIN engine_access_grant_revocations r ON r.request_id=a.entity_id WHERE a.event_name='system.engine_access_grant.revoked' AND a.principal_id=r.revoked_by_principal_id AND a.tenant_id=7")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs a JOIN engine_access_grants g ON g.request_id=a.entity_id WHERE a.event_name='system.engine_access_grant.issued' AND a.principal_id=$catalog_id AND a.tenant_id=7 AND g.approval_mode='catalog'")" = 2
