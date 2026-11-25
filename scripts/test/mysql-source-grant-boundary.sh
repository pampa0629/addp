#!/usr/bin/env bash
# Exercise the shipped procedure against tables and provenance guards from the System installer.
set -euo pipefail
container=$1
work=$2
system_backend="$(cd "$(dirname "$0")/../../system/backend" && pwd)"
runtime_password=$(openssl rand -hex 24)
export ADDP_DATABASE_RUNTIME_USER=system_gate ADDP_DATABASE_RUNTIME_PASSWORD="$runtime_password"
admin() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB system'
}
export ADDP_INFRA_DATABASE_PROVIDER=mysql ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$ADDP_TEST_MYSQL_PORT"
export ADDP_DATABASE_DEPLOY_USER=source_installer
ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
export ADDP_DATABASE_DEPLOY_PASSWORD
admin <<SQL
CREATE USER 'source_installer'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'system_gate'@'%' IDENTIFIED BY '$runtime_password';
SQL
admin <<'SQL'
GRANT ALL ON `system`.* TO 'source_installer'@'%' WITH GRANT OPTION;
SQL
result=0
"$work/system-server" prepare-database system > "$work/source-triggers.sql" || result=$?
test "$result" = 3
admin < "$work/source-triggers.sql"
"$work/system-server" prepare-database system > "$work/source-replay.sql"
test ! -s "$work/source-replay.sql"
# Reconstruct each previously deployed guard in the gate-owned database.
for revision in 4 5; do
    old_guard=source_grant_catalog_node
    if [ "$revision" = 4 ]; then old_guard=source_grant_node_target; fi
    admin <<<"DROP TRIGGER source_grant_namespace_node"
    {
        echo 'DELIMITER $$'
        awk -v revision="$revision" '
            NR==1 { sub("CREATE TRIGGER","CREATE DEFINER=`source_installer`@`%` TRIGGER") }
            { print }
            NR==2 && revision==4 { print "PRECEDES source_grant_item_target" }
            END { print "$$" }
        ' "$system_backend/internal/migration/mysql_source_grant_node_v${revision}.sql"
        echo 'DELIMITER ;'
    } | admin
    admin <<<"UPDATE startup_schema_revision SET version=$revision,pending_version=0 WHERE owner='system_source_grant_catalog'"
    result=0
    "$work/system-server" prepare-database system > "$work/node-upgrade.sql" || result=$?
    test "$result" = 3
    guard_count="SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema='system' AND trigger_name='$old_guard'"
    test "$(admin <<<"$guard_count")" = 1
    admin < "$work/node-upgrade.sql"
    result=0
    "$work/system-server" prepare-database system > "$work/node-retire.sql" || result=$?
    if [ "$result" = 3 ]; then
        admin < "$work/node-retire.sql"
        "$work/system-server" prepare-database system
    else
        test "$result" = 0
    fi
    test "$(admin <<<"$guard_count")" = 0
    test "$(admin <<<"SELECT version FROM startup_schema_revision WHERE owner='system_source_grant_catalog' AND pending_version=0")" = 6
    "$work/system-server" prepare-database system > "$work/node-replay.sql"
    test ! -s "$work/node-replay.sql"
done
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
       (99,7,'Grant files','nfs',JSON_OBJECT(),JSON_OBJECT('id',99)),
       (98,7,'Grant objects','minio',JSON_OBJECT(),JSON_OBJECT('id',98)),
       (81,7,'Catalog schema','gaussdb',JSON_OBJECT(),JSON_OBJECT('id',81)),
       (82,7,'Catalog directory','ftp',JSON_OBJECT(),JSON_OBJECT('id',82)),
       (83,7,'Catalog prefix','rustfs',JSON_OBJECT(),JSON_OBJECT('id',83)),
       (84,7,'Catalog database','mysql',JSON_OBJECT(),JSON_OBJECT('id',84)),
       (85,7,'Document database','mongodb',JSON_OBJECT(),JSON_OBJECT('id',85));
CREATE TABLE sample_audit (id INT PRIMARY KEY);
SQL
# Refuse a deployment with an inherited write path, even at a current revision.
admin <<<"GRANT INSERT ON \`system\`.engine_access_grants TO 'system_gate'@'%'"
if "$work/system-server" prepare-database system >"$work/unexpected.out" 2>"$work/direct-write.err"; then exit 1; fi
grep -q 'expected runtime write denial' "$work/direct-write.err"
test "$(admin <<<"SELECT pending_version FROM startup_schema_revision WHERE owner='system_source_grant_procedure'")" -gt 0
admin <<<"REVOKE INSERT ON \`system\`.engine_access_grants FROM 'system_gate'@'%'"
transfer_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-transfer'")
manager_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-manager'")
catalog_id=$(admin <<<"SELECT service_principal_id FROM oauth_clients WHERE client_id='addp-catalog'")
human_version=$(admin <<<"SELECT authorization_version FROM principals WHERE id=40001")
# A repeat preparation grants DML on new ordinary tables, including sample_audit.
"$work/system-server" prepare-database system
caller() {
    local user=$1
    shift
    MYSQL_PWD="$runtime_password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -u"$user" -NB "$@"
}

# --force keeps the same connection for the explicit post-error COMMIT or ROLLBACK.
expect_error() {
    local user=$1 code=$2 statements=$3
    caller "$user" --force <<<"$statements" >"$work/boundary.out" 2>"$work/boundary.err" || true
    test "$(grep -c '^ERROR ' "$work/boundary.err")" = 1
    grep -q "^ERROR $code " "$work/boundary.err" || { cat "$work/boundary.err" >&2; return 1; }
}
test "$(admin <<<"SELECT CONCAT(SECURITY_TYPE, '/', DEFINER) FROM information_schema.routines WHERE routine_schema='system' AND routine_name='persist_source_grant'")" = 'DEFINER/source_installer@%'
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
# Use the installed procedure with the same restricted caller for every node target.
node_call() {
    local engine=$1 path=$2 recursive=$3
    cat <<SQL
CALL system.persist_source_grant('independent',$(uuid 120),7,$engine,$path,NULL,$recursive,
    '','',NULL,NULL,NULL,'user',40001,'read','until_revoked',NULL,40001,50001,$human_version,'node fixture',
    NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)
SQL
}
schema_node="JSON_REMOVE($table_path,'$.segments[2]')"
nfs_root="JSON_REMOVE($file_path,'$.segments[1]')"
service_root="JSON_SET($nfs_root,'$.engine_id',98,'$.segments[0].kind','service','$.segments[0].term','service')"
directory="JSON_ARRAY_APPEND($nfs_root,'$.segments',JSON_OBJECT('kind','directory','term','directory','name','docs'))"
bucket="JSON_ARRAY_APPEND($service_root,'$.segments',JSON_OBJECT('kind','bucket','term','bucket','name','reports'))"
prefix="JSON_ARRAY_APPEND($bucket,'$.segments',JSON_OBJECT('kind','prefix','term','prefix','name','quarter'))"
database_node="JSON_SET($schema_node,'$.engine_id',84,'$.segments[1].term','database')"
for input in "84 $database_node 0" "12 $schema_node 0" "99 $directory 1" "98 $service_root 1" "98 $prefix 0" \
    "81 JSON_SET($schema_node,'$.engine_id',81) 0" "82 JSON_SET($directory,'$.engine_id',82) 1" \
    "83 JSON_SET($service_root,'$.engine_id',83) 1" "83 JSON_SET($prefix,'$.engine_id',83) 0"; do
    read -r engine path recursive <<<"$input"
    actual=$(caller system_gate <<<"START TRANSACTION; $(node_call "$engine" "$path" "$recursive");
        SELECT COUNT(*) FROM system.engine_access_grants WHERE request_id=$(uuid 120) AND node_recursive=$recursive; ROLLBACK;")
    test "$actual" = 1
done
for input in "84 JSON_SET($database_node,'$.segments[1].term','table') 1" "12 $table_path 1" "98 $service_root 0" "99 $service_root 1" \
    "99 JSON_SET($directory,'$.segments[1].name','a/b') 1" "99 JSON_SET($directory,'$.segments[1].name',23) 1" \
    "99 JSON_SET($directory,'$.extra',1) 1"; do
    read -r engine path recursive <<<"$input"
    expect_error system_gate 1644 "START TRANSACTION; $(node_call "$engine" "$path" "$recursive"); COMMIT;"
    grep -q 'source_grant_node_' "$work/boundary.err"
done
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE request_id=$(uuid 120)")" = 0
echo 'System node Grants: MySQL databases, PostgreSQL/GaussDB schemas, NFS/FTP directories and MinIO/RustFS object scopes checked through restricted procedure'
caller system_gate system <<SQL
SET @recorded=NOW(6);
INSERT INTO engine_access_fulfillment_outcomes (request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,outcome,recorded_at,deadline)
VALUES ($(uuid 13),7,12,$catalog_id,$table_path,
  JSON_OBJECT('decision_id',$(uuid 113),'expiry_mode','until_revoked','expires_at',NULL,'item_fingerprint',SHA2('12:public.orders',256),'boundary_hash',SHA2('receipt',256),
    'recipient_type','user','recipient_id',40001,'action','read',
    'operator',JSON_OBJECT('principal_id','40001','tenant_membership_id','50001','authorization_version','$human_version')),
  'until_revoked','accepted',@recorded,@recorded+INTERVAL 5 MINUTE);
SQL
# Exercise the procedure and receipt table separately. A valid-looking SHA-256
# for a different path must be rejected at either persistence boundary.
independent=$(call independent "$(uuid 121)" 0 NULL)
wrong_fp="SHA2('different-item',256)"
correct_fp="SHA2('12:public.independent_orders',256)"
wrong="${independent/"$correct_fp"/$wrong_fp}"
test "$wrong" != "$independent"
expect_error system_gate 1644 "START TRANSACTION; $wrong; COMMIT;"
grep -q 'source_item_fingerprint' "$work/boundary.err"
expect_error system_gate 1644 "INSERT INTO system.engine_access_fulfillment_outcomes
    (request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,outcome,recorded_at,deadline)
    SELECT $(uuid 122),tenant_id,engine_id,caller_principal_id,catalog_path,
    JSON_SET(binding,'$.item_fingerprint',SHA2('different-item',256)),expiry_mode,outcome,recorded_at,deadline
    FROM system.engine_access_fulfillment_outcomes WHERE request_id=$(uuid 13);"
grep -q 'source_item_fingerprint' "$work/boundary.err"
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_fulfillment_outcomes WHERE request_id=$(uuid 122)")" = 0
precise_call() {
    local engine=$1 path=$2 fingerprint=$3 composition=$4 scope=$5 metadata=$6 action=$7
    cat <<SQL
CALL system.persist_source_grant('independent',$(uuid 125),7,$engine,$path,NULL,NULL,
    $fingerprint,SHA2('boundary',256),$composition,$scope,$metadata,'user',40001,'$action','until_revoked',
    NULL,40001,50001,$human_version,'target fixture',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)
SQL
}
filegdb="JSON_SET($directory,'$.segments[1].name','survey.gdb')"
whole="JSON_OBJECT('format','filegdb','rule_version',1,'root',$filegdb)"
object="JSON_ARRAY_APPEND($bucket,'$.segments',JSON_OBJECT('kind','object','term','object','name','data.bin'))"
metadata="JSON_OBJECT('target',$table_path,'layout','single','data_type','table','format','','composition_digest',SHA2('m',256))"
raster="JSON_SET($file_path,'$.segments[1].name','scene.tif')"
sidecar="JSON_SET($file_path,'$.segments[1].name','scene.tif.msk')"
refs="JSON_ARRAY(JSON_OBJECT('target',$raster,'primary',true,'required',true,'role','image'),JSON_OBJECT('target',$sidecar,'primary',false,'required',false,'role','mask'))"
tiff="JSON_OBJECT('format','tiff','source_layout','multi','refs',$refs)"
for target_call in \
    "$(precise_call 99 "$filegdb" "SHA2('99:survey.gdb',256)" NULL "$whole" NULL read)" \
    "$(precise_call 98 "$object" "SHA2('98:reports/data.bin',256)" NULL NULL NULL read)" \
    "$(precise_call 12 "$table_path" "SHA2('12:public.orders',256)" NULL NULL "$metadata" metadata_read)" \
    "$(precise_call 99 "$raster" "SHA2('99:scene.tif',256)" "$tiff" NULL NULL read)"; do
    test "$(caller system_gate <<<"START TRANSACTION; $target_call; SELECT COUNT(*) FROM system.engine_access_grants WHERE request_id=$(uuid 125); ROLLBACK;")" = 1
done
for target_call in \
    "$(precise_call 99 "$filegdb" "SHA2('99:survey.gdb',256)" NULL NULL NULL read)" \
    "$(precise_call 12 "$table_path" "SHA2('12:public.orders',256)" NULL NULL "$metadata" read)" \
    "$(precise_call 99 "$raster" "SHA2('99:scene.tif',256)" "JSON_SET($tiff,'$.refs[1].target',$raster)" NULL NULL read)" \
    "$(precise_call 99 "$raster" "SHA2('99:scene.tif',256)" "JSON_SET($tiff,'$.refs[1].target',$object)" NULL NULL read)"; do
    expect_error system_gate 1644 "START TRANSACTION; $target_call; COMMIT;"
    grep -Eq 'source_(item|grant)_' "$work/boundary.err"
done
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE request_id=$(uuid 125)")" = 0
receipt=system.engine_access_fulfillment_outcomes
test "$(caller system_gate <<<"SELECT outcome FROM $receipt WHERE request_id=$(uuid 13)")" = accepted
for statement in "UPDATE $receipt SET outcome='closed' WHERE request_id=$(uuid 13)" "DELETE FROM $receipt WHERE request_id=$(uuid 13)"; do
    expect_error system_gate 1644 "$statement"
    # The table's immutable triggers must reject even an account with DML privileges.
    if admin <<<"$statement" >"$work/immutable.out" 2>"$work/immutable.err"; then exit 1; fi
    grep -q '^ERROR 1644 ' "$work/immutable.err"
done
expect_error system_gate 1142 "TRUNCATE TABLE $receipt;"
pair=$(call task_production "$(uuid 11)" 1 NULL)
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
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_grants WHERE request_id=$(uuid 16)")" = 0
test "$(admin <<<"SELECT COUNT(*) FROM engine_access_upload_grant_outcomes WHERE upload_id=$(uuid 6)")" = 0
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
admin <<'SQL'
DELIMITER $$
CREATE TRIGGER gate_refuse_initial_audit BEFORE INSERT ON audit_logs FOR EACH ROW
BEGIN
    IF NEW.event_name='system.engine_access_initial_grant.issued' AND NEW.entity_id='00000000-0000-4000-8000-000000000501' THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='gate rejected initial grant audit';
    END IF;
END$$
DELIMITER ;
SQL
(cd "$system_backend" && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_CATALOG_PRINCIPAL_ID="$catalog_id" ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
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
SQL
(cd "$system_backend" && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
    go test ./internal/engineaccess -run '^TestEngineDelegationAgainstMySQL$' -count=1 -timeout=60s -v) | tee "$work/delegation.log"
grep -q '^--- PASS: TestEngineDelegationAgainstMySQL ' "$work/delegation.log"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs WHERE entity_type='engine_access_delegation' AND tenant_id=7 AND principal_id=40001 AND event_name IN ('system.engine_access_delegation.created','system.engine_access_delegation.revoked')")" = 2
test "$(admin <<<"SELECT COUNT(*) FROM refresh_token_families f JOIN principals p ON p.id=f.principal_id WHERE f.principal_id=40002 AND f.revoked_at IS NOT NULL AND f.revoked_reason='engine_access_delegation_changed' AND p.authorization_version=f.issued_authorization_version+2")" = 1
delegations=system.engine_access_delegations
expect_error system_gate 1644 "UPDATE $delegations SET grant_reason='changed' WHERE id=1;"
expect_error system_gate 1644 "DELETE FROM $delegations WHERE id=1;"
expect_error system_gate 1142 "TRUNCATE TABLE $delegations;"
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
SQL
(cd "$system_backend" && GOWORK=off ADDP_TEST_MYSQL_SOURCE_GRANT=1 ADDP_TEST_MYSQL_PASSWORD="$runtime_password" \
    go test ./internal/engineaccess -run '^TestCatalogFulfillmentServiceAgainstMySQL$' -count=1 -timeout=60s -v) | tee "$work/catalog-fulfillment.log"
grep -q '^--- PASS: TestCatalogFulfillmentServiceAgainstMySQL ' "$work/catalog-fulfillment.log"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs a JOIN engine_access_fulfillment_outcomes o ON o.request_id=a.entity_id WHERE a.event_name='system.engine_access_fulfillment.accepted' AND a.principal_id=$catalog_id AND o.outcome='accepted' AND o.binding->>'$.operator.principal_id'='40003'")" = 2
revocations=system.engine_access_grant_revocations
for stmt in "UPDATE $revocations SET reason='rewrite'" "DELETE FROM $revocations"; do
    expect_error system_gate 1644 "$stmt"
    if admin <<<"$stmt" >"$work/immutable.out" 2>"$work/immutable.err"; then exit 1; fi
    grep -q '^ERROR 1644 ' "$work/immutable.err"
done
expect_error system_gate 1142 "TRUNCATE TABLE $revocations;"
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs a JOIN engine_access_grant_revocations r ON r.request_id=a.entity_id WHERE a.event_name='system.engine_access_grant.revoked' AND a.principal_id=r.revoked_by_principal_id AND a.tenant_id=7")" = 1
test "$(admin <<<"SELECT COUNT(*) FROM audit_logs a JOIN engine_access_grants g ON g.request_id=a.entity_id WHERE a.event_name='system.engine_access_grant.issued' AND a.principal_id=$catalog_id AND a.tenant_id=7 AND g.approval_mode='catalog'")" = 2
