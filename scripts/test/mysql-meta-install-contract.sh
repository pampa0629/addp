#!/usr/bin/env bash
# Exercise both deployment commands in the gate's disposable MySQL instance.
set -euo pipefail
work=$1
container=$2
export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3" ADDP_INFRA_DATABASE_PROVIDER=mysql
admin() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB meta'
}
system_password=$(openssl rand -hex 24)
meta_password=$(openssl rand -hex 24)
admin <<SQL
CREATE DATABASE \`system\`;
CREATE USER 'system_setup'@'%' IDENTIFIED BY '$system_password';
GRANT SELECT, INSERT, UPDATE, CREATE, REFERENCES, INDEX, ALTER, TRIGGER, CREATE ROUTINE, EXECUTE ON \`system\`.* TO 'system_setup'@'%';
CREATE USER 'meta_setup'@'%' IDENTIFIED BY '$meta_password';
GRANT SELECT, INSERT, UPDATE, CREATE, REFERENCES, INDEX, ALTER ON meta.* TO 'meta_setup'@'%';
SQL

export ADDP_MYSQL_INSTALL_USER=system_setup ADDP_MYSQL_INSTALL_PASSWORD="$system_password"
result=0
"$work/system-server" install-mysql-system >"$work/system-triggers.sql" || result=$?
test "$result" = 3
admin <"$work/system-triggers.sql"
"$work/system-server" install-mysql-system

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
export ADDP_MYSQL_INSTALL_USER=meta_setup ADDP_MYSQL_INSTALL_PASSWORD="$meta_password"
if "$work/meta-server" install-mysql-scan 2>"$work/meta-denied.err"; then exit 1; fi
grep -Eiq '1142.*REFERENCES|REFERENCES.*denied' "$work/meta-denied.err"
test "$(echo "SELECT version=0 AND pending_version=1 FROM startup_schema_revision WHERE owner='meta_scan_catalog'" | admin)" = 1
admin <<'SQL'
GRANT REFERENCES ON `system`.engines TO 'meta_setup'@'%';
SQL
# The DBA connection owns the grant, including revocation on installation failure.
revoke_parent() {
    echo "REVOKE REFERENCES ON \`system\`.engines FROM 'meta_setup'@'%'" | admin
}
trap revoke_parent EXIT
"$work/meta-server" install-mysql-scan
# An already published revision must not hide a missing key on installation replay.
admin <<'SQL'
ALTER TABLE meta_item DROP FOREIGN KEY fk_meta_item_tenant_engine;
SQL
if "$work/meta-server" install-mysql-scan 2>"$work/meta-key.err"; then exit 1; fi
grep -q 'requires both tenant/engine foreign keys' "$work/meta-key.err"
MYSQL_PWD="$meta_password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -umeta_setup meta <<'SQL'
ALTER TABLE meta_item ADD CONSTRAINT fk_meta_item_tenant_engine FOREIGN KEY (tenant_id, engine_id)
    REFERENCES `system`.engines(tenant_id, id) ON DELETE CASCADE;
SQL
"$work/meta-server" install-mysql-scan
revoke_parent
trap - EXIT

# Prove the same installation identity can no longer create a parent reference.
if MYSQL_PWD="$meta_password" docker exec -i -e MYSQL_PWD "$container" mysql --protocol=TCP -h127.0.0.1 -umeta_setup meta \
    -e 'CREATE TABLE ref_probe (tenant_id BIGINT, engine_id BIGINT, FOREIGN KEY (tenant_id,engine_id) REFERENCES `system`.engines(tenant_id,id))' 2>"$work/revoked.err"; then exit 1; fi
grep -Eiq '1142.*REFERENCES|REFERENCES.*denied' "$work/revoked.err"
echo 'Verified System engine installation and address uniqueness; Meta REFERENCES denied, granted for installation, both keys verified, then revoked'
