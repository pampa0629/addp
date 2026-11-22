#!/usr/bin/env bash
# Run only against a new, gate-owned PostgreSQL instance.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
name="addp-meta-fact-$$"
cleanup() {
    if [[ -f "$work/container" ]]; then docker rm -fv "$(cat "$work/container")" >/dev/null; fi
    rm -rf "$work"
}
trap cleanup EXIT
export POSTGRES_PASSWORD
POSTGRES_PASSWORD=$(openssl rand -hex 24)
export POSTGRES_DB=addp_test POSTGRES_HOST=127.0.0.1
docker run -d --cidfile "$work/container" --name "$name" -e POSTGRES_DB -e POSTGRES_PASSWORD \
    -p 127.0.0.1::5432 postgres:15 >/dev/null
dba() {
    docker exec -i -e PGPASSWORD="$POSTGRES_PASSWORD" "$name" psql -h127.0.0.1 -U postgres -d addp_test -X -qAt -v ON_ERROR_STOP=1 "$@"
}
for ((attempt=0;attempt<90;attempt++)); do
    if [[ "$(dba -c 'SELECT current_database()' 2>/dev/null)" == addp_test ]]; then break; fi
    sleep 1
done
[[ "$(dba -c 'SELECT current_database()')" == addp_test ]]
export POSTGRES_PORT
POSTGRES_PORT=$(docker port "$name" 5432/tcp | cut -d: -f2)
export ADDP_INFRA_DATABASE_PROVIDER=postgres ADDP_PG_META_INSTALL_PASSWORD
ADDP_PG_META_INSTALL_PASSWORD=$(openssl rand -hex 24)
dba -v pw="$ADDP_PG_META_INSTALL_PASSWORD" <<'SQL'
CREATE SCHEMA system;
CREATE ROLE meta_install LOGIN PASSWORD :'pw';
CREATE SCHEMA meta AUTHORIZATION meta_install;
SQL
dba < "$root/system/backend/internal/migration/sql/000001_iam_identity_tenant.up.sql"
dba <<'SQL'
GRANT USAGE ON SCHEMA system TO meta_install;
GRANT SELECT (tenant_id,id), REFERENCES (tenant_id,id) ON system.engines TO meta_install;
SQL
(cd "$root/meta/backend" && ADDP_TEST_PG_FACT_AUTHORITY=1 GOWORK=off go test ./internal/repository -run '^TestPreparePostgresFactAuthorityFixture$' -count=1 -v) | tee "$work/schema.log"
grep -q '^--- PASS: TestPreparePostgresFactAuthorityFixture ' "$work/schema.log"
export ADDP_TEST_META_WORKER_PASSWORD ADDP_TEST_META_SOURCE_PASSWORD
ADDP_TEST_META_WORKER_PASSWORD=$(openssl rand -hex 24)
ADDP_TEST_META_SOURCE_PASSWORD=$(openssl rand -hex 24)
dba -v wp="$ADDP_TEST_META_WORKER_PASSWORD" -v sp="$ADDP_TEST_META_SOURCE_PASSWORD" <<'SQL'
CREATE ROLE meta_fact_owner NOLOGIN;
GRANT meta_fact_owner TO meta_install;
CREATE ROLE meta_gate_worker LOGIN PASSWORD :'wp';
CREATE ROLE source_gate_reader LOGIN PASSWORD :'sp';
CREATE SCHEMA scan_source;
CREATE TABLE scan_source.orders(id bigint PRIMARY KEY, label text);
INSERT INTO scan_source.orders VALUES(1,'scanned');
GRANT USAGE ON SCHEMA scan_source TO source_gate_reader;
GRANT SELECT ON scan_source.orders TO source_gate_reader;
GRANT USAGE ON SCHEMA meta TO meta_gate_worker;
GRANT SELECT,INSERT,UPDATE ON meta.meta_node,meta.meta_item,meta.object_index_deletions TO meta_gate_worker;
GRANT USAGE ON SEQUENCE meta.meta_node_id_seq,meta.meta_item_id_seq TO meta_gate_worker;
INSERT INTO system.engines(id,tenant_id,name,engine_type,connection_info) VALUES(101,7,'gate','postgresql','{}');
SQL
install_sql() {
    docker exec -i -e PGPASSWORD="$ADDP_PG_META_INSTALL_PASSWORD" "$name" psql -h127.0.0.1 -U meta_install -d addp_test -X -qAt -v ON_ERROR_STOP=1
}
for pass in 1 2; do
    {
        echo 'BEGIN; SET LOCAL search_path=pg_catalog,pg_temp; SET LOCAL addp.meta_trigger_owner=meta_fact_owner;'
        cat "$root/meta/backend/internal/repository/postgres_fact_triggers.sql"
        echo 'COMMIT;'
    } | install_sql
done
[[ "$(dba -c "SELECT count(*) FROM pg_proc WHERE oid IN ('meta.capture_data_item_change()'::regprocedure,'meta.advance_authorization_basis_revision()'::regprocedure) AND proowner='meta_fact_owner'::regrole AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog, pg_temp'] AND NOT has_function_privilege('meta_gate_worker',oid,'EXECUTE')")" == 2 ]]
[[ "$(dba -c "SELECT NOT has_schema_privilege('meta_fact_owner','meta','CREATE') AND NOT pg_has_role('meta_gate_worker','meta_fact_owner','MEMBER')")" == t ]]
(cd "$root/meta/backend" && ADDP_TEST_PG_FACT_AUTHORITY=1 GOWORK=off go test ./internal/scanruntime -run '^TestPostgresWorkerScanFactAuthority$' -count=1 -v) | tee "$work/scan.log"
grep -q '^--- PASS: TestPostgresWorkerScanFactAuthority ' "$work/scan.log"
dba <<'SQL'
SELECT json_build_object('tenant',r.tenant_id,'revision',r.version,'item_id',i.id,'table',i.name,'catalog',c.operation)
FROM meta.authorization_basis_revisions r JOIN meta.meta_item i USING(tenant_id)
JOIN meta.data_item_changes c ON c.item_id=i.id AND c.source_identity=i.fingerprint
WHERE r.tenant_id=7 AND i.engine_id=101 AND i.name='orders';
SQL
[[ "$(dba -c "SELECT count(*) FROM meta.meta_item i JOIN meta.meta_node n ON i.node_id=n.id AND i.tenant_id=n.tenant_id JOIN meta.data_item_changes c ON c.item_id=i.id AND c.source_identity=i.fingerprint JOIN meta.authorization_basis_revisions r ON r.tenant_id=i.tenant_id WHERE r.tenant_id=7 AND r.version>0 AND r.version<>111 AND n.name='scan_source' AND i.name='orders' AND c.operation='upsert' AND c.snapshot->>'table_name'='orders'")" -ge 1 ]]

facts() {
    dba -c "SELECT tenant_id,version FROM meta.authorization_basis_revisions ORDER BY tenant_id" \
        -c "SELECT id,tenant_id,item_id,source_identity,operation,snapshot FROM meta.data_item_changes ORDER BY id"
}
facts > "$work/before"
for write in "UPDATE meta.authorization_basis_revisions SET version=111 WHERE tenant_id=7" \
    "INSERT INTO meta.data_item_changes(tenant_id,item_id,source_identity,operation,snapshot) VALUES(7,1,'direct','upsert','{}')"; do
    if docker exec -e PGPASSWORD="$ADDP_TEST_META_WORKER_PASSWORD" "$name" psql -h127.0.0.1 -U meta_gate_worker -d addp_test -X -v VERBOSITY=sqlstate -v ON_ERROR_STOP=1 -c "$write" 2>"$work/denied"; then
        echo 'Worker unexpectedly wrote derived facts' >&2
        exit 1
    fi
    grep -Eq '^ERROR: +42501$' "$work/denied"
done
facts > "$work/after"
cmp "$work/before" "$work/after"
echo 'Worker direct writes rejected (42501); committed tenant versions and Catalog changes unchanged.'
