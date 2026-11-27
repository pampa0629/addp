#!/usr/bin/env bash
# ADDP_T2_INPUT_FILES=scripts/test/meta-database-restart-contract.py
# Meta installation runs with the platform deployment role against an isolated PostgreSQL instance.
set -euo pipefail
mode=${1:---fresh}
case "$mode" in --fresh|--upgrade) ;; *) echo 'expected --fresh or --upgrade' >&2; exit 2;; esac
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
name="addp-meta-install-$$"
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
(cd "$root/common" && SCHEMA_POSTGRES_TEST_DSN="postgres://postgres:$POSTGRES_PASSWORD@127.0.0.1:$POSTGRES_PORT/addp_test?sslmode=disable" GOWORK=off go test ./schema -run '^TestPostgresStartupSchemaOwnership$' -count=1 -v)
export ADDP_INFRA_DATABASE_PROVIDER=postgres ADDP_DATABASE_DEPLOY_USER=meta_install ADDP_DATABASE_DEPLOY_PASSWORD
ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
dba -v role_password="$ADDP_DATABASE_DEPLOY_PASSWORD" <<'SQL'
CREATE SCHEMA system;
CREATE ROLE meta_install LOGIN PASSWORD :'role_password';
CREATE SCHEMA meta AUTHORIZATION meta_install;
SQL
dba < "$root/system/backend/internal/migration/sql/000001_iam_identity_tenant.up.sql"
# The fixture runs Common's published migration with its deployment connection.
(cd "$root/meta/backend" && ADDP_TEST_META_COMMON_INSTALL=1 GOWORK=off go test ./internal/repository -count=1 -run '^TestPrepareCommonForIsolatedMetaInstall$' -v) | tee "$work/common.log"
grep -q '^--- PASS: TestPrepareCommonForIsolatedMetaInstall ' "$work/common.log"
dba <<'SQL'
GRANT USAGE ON SCHEMA common,system TO meta_install;
GRANT SELECT ON common.startup_schema_revision, common.task_executions TO meta_install;
GRANT SELECT (tenant_id,id), REFERENCES (tenant_id,id) ON system.engines TO meta_install;
SQL
(cd "$root/meta/backend" && GOWORK=off go build -o "$work/meta-server" ./cmd/server)
if [[ "$mode" == --upgrade ]]; then
    # The last released Backend before ownership moved to the deployment entry.
    base=eec2bac5dcb06bb9ebda42fb30bc9540de62ead8
    mkdir "$work/previous"
    git -C "$root" archive "$base" common meta | tar -xf - -C "$work/previous"
    (cd "$work/previous/meta/backend" && GOWORK=off go build -o "$work/previous-server" ./cmd/server)
    dba -c 'GRANT CREATE ON DATABASE addp_test TO meta_install'
    POSTGRES_USER=meta_install POSTGRES_PASSWORD="$ADDP_DATABASE_DEPLOY_PASSWORD" python3 - "$work" "$name" <<'PYOLD'
from pathlib import Path
import os, subprocess, sys, time

work=Path(sys.argv[1])
env=dict(os.environ, PROJECT_ROOT=str(work), META_BACKEND_PORT="0", META_SERVICE_CLIENT_SECRET="x"*32,
         SYSTEM_URL="http://127.0.0.1:1", REDIS_HOST="127.0.0.1", REDIS_PORT="1")
with (work/"previous.log").open("w") as log:
    p=subprocess.Popen([work/"previous-server"],cwd=work,env=env,stdout=log,stderr=log)
    try:
        for _ in range(120):
            probe=subprocess.run(["docker","exec","-e","PGPASSWORD",sys.argv[2],"psql","-h127.0.0.1","-U","meta_install","-d","addp_test","-qAtX","-c","SELECT version FROM meta.startup_schema_revision"],
                env=dict(os.environ,PGPASSWORD=env["POSTGRES_PASSWORD"]),capture_output=True,text=True,timeout=5)
            if probe.returncode==0 and probe.stdout.strip()=="7":
                break
            if p.poll() is not None: raise SystemExit(f"v7 Backend exited before publishing schema: {p.returncode}")
            time.sleep(.25)
        else: raise SystemExit("v7 Backend did not publish the expected revision")
    finally:
        if p.poll() is None: p.terminate()
        try: p.wait(timeout=10)
        except subprocess.TimeoutExpired: p.kill();p.wait()
PYOLD
    dba <<'SQL'
REVOKE CREATE ON DATABASE addp_test FROM meta_install;
INSERT INTO system.engines(id,tenant_id,name,engine_type,connection_info) VALUES(71,9,'upgrade source','postgresql','{}');
SET ROLE meta_install;
INSERT INTO meta.meta_node(tenant_id,engine_id,node_type,name,depth) VALUES(9,71,'schema','upgrade_source',0);
INSERT INTO meta.meta_item(tenant_id,engine_id,node_id,item_type,name,fingerprint,attributes)
SELECT 9,71,id,'table','already_scanned','pg-v7-kept','{}' FROM meta.meta_node WHERE tenant_id=9;
INSERT INTO meta.lineage_service_dependencies(tenant_id, source_item_id, service_id, published_revision, dependency_kind, first_observed_at, last_observed_at, service_updated_at)
SELECT 9,id,71,'prior','table',now(),now(),'2026-01-01 00:00:00+00' FROM meta.meta_item WHERE fingerprint='pg-v7-kept';
SQL
    prior_facts() {
        dba -c "SELECT i.id,i.fingerprint,c.id,c.operation,r.version FROM meta.meta_item i JOIN meta.data_item_changes c ON c.item_id=i.id JOIN meta.authorization_basis_revisions r ON r.tenant_id=i.tenant_id WHERE i.tenant_id=9 ORDER BY c.id"
    }
    prior_facts > "$work/prior"
    test -s "$work/prior"
fi
"$work/meta-server" prepare-database
[[ "$(dba -c 'SELECT version FROM meta.startup_schema_revision')" == 10 ]]
dba -c 'ALTER ROLE meta_install SET default_transaction_read_only TO on'
"$work/meta-server" prepare-database
dba -c 'ALTER ROLE meta_install RESET default_transaction_read_only'
if [[ "$mode" == --upgrade ]]; then
    [[ "$(dba -c "SELECT count(*) FROM meta.lineage_service_publication_times WHERE tenant_id=9 AND service_id=71 AND service_updated_at='2026-01-01 00:00:00+00'")" == 1 ]]
    prior_facts > "$work/current"
    cmp "$work/prior" "$work/current"
    echo 'Released PG v7 Backend -> deployment revision 10: item ID, Catalog cursor and tenant basis preserved.'
fi
echo 'Meta installation and repeat preparation completed against PostgreSQL with the System parent key owned separately.'
export ADDP_TEST_META_WORKER_PASSWORD ADDP_TEST_META_SOURCE_PASSWORD
ADDP_TEST_META_WORKER_PASSWORD=$(openssl rand -hex 24)
ADDP_TEST_META_SOURCE_PASSWORD=$(openssl rand -hex 24)
dba -v wp="$ADDP_TEST_META_WORKER_PASSWORD" -v sp="$ADDP_TEST_META_SOURCE_PASSWORD" <<'SQL'
CREATE ROLE meta_gate_worker LOGIN PASSWORD :'wp';
CREATE ROLE source_gate_reader LOGIN PASSWORD :'sp';
CREATE SCHEMA scan_source;
CREATE TABLE scan_source.orders(id bigint PRIMARY KEY, label text);
INSERT INTO scan_source.orders VALUES(1,'scanned');
GRANT USAGE ON SCHEMA scan_source TO source_gate_reader;
GRANT SELECT ON scan_source.orders TO source_gate_reader;
GRANT USAGE ON SCHEMA common,meta TO meta_gate_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA common,meta TO meta_gate_worker;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA common,meta TO meta_gate_worker;
INSERT INTO system.engines(id,tenant_id,name,engine_type,connection_info) VALUES(101,7,'gate','postgresql','{}');
SQL
(cd "$root/meta/backend" && ADDP_TEST_PG_FACT_AUTHORITY=1 GOWORK=off go test ./internal/scanruntime -run '^TestPostgresWorkerScanFactAuthority$' -count=1 -v) | tee "$work/scan.log"
grep -q '^--- PASS: TestPostgresWorkerScanFactAuthority ' "$work/scan.log"
dba <<'SQL'
SELECT json_build_object('tenant',r.tenant_id,'revision',r.version,'item_id',i.id,'table',i.name,'catalog',c.operation)
FROM meta.authorization_basis_revisions r JOIN meta.meta_item i USING(tenant_id)
JOIN meta.data_item_changes c ON c.item_id=i.id AND c.source_identity=i.fingerprint
WHERE r.tenant_id=7 AND i.engine_id=101 AND i.name='orders';
SQL
[[ "$(dba -c "SELECT count(*) FROM meta.meta_item i JOIN meta.meta_node n ON i.node_id=n.id AND i.tenant_id=n.tenant_id JOIN meta.data_item_changes c ON c.item_id=i.id AND c.source_identity=i.fingerprint JOIN meta.authorization_basis_revisions r ON r.tenant_id=i.tenant_id WHERE r.tenant_id=7 AND r.version>0 AND r.version<>111 AND n.name='scan_source' AND i.name='orders' AND c.operation='upsert' AND c.snapshot->>'table_name'='orders'")" -ge 1 ]]
[[ "$(dba -c "SELECT count(*) FROM pg_proc WHERE oid IN ('meta.capture_data_item_change()'::regprocedure,'meta.advance_authorization_basis_revision()'::regprocedure,'meta.stale_lineage_for_changed_fields()'::regprocedure,'meta.stale_lineage_for_soft_deleted_item()'::regprocedure) AND proowner='meta_install'::regrole AND NOT prosecdef AND proconfig=ARRAY['search_path=pg_catalog, pg_temp']")" == 4 ]]
if [[ "$(dba -c "SELECT has_schema_privilege('meta_gate_worker','meta','CREATE')")" != f ]]; then
    echo 'runtime account can alter the Meta schema' >&2
    exit 1
fi
dba <<'SQL'
INSERT INTO meta.lineage_item_relations(tenant_id,source_item_id,target_item_id,relation_kind,granularity,status,first_observed_at,last_observed_at,updated_at,source_field_name,target_field_name,source_schema_hash,target_schema_hash,transformation)
SELECT 7,i.id,i.id,'derive','field',s.status,now(),now(),'2001-01-01','label','id','src','dst','direct'
FROM meta.meta_item i CROSS JOIN (VALUES ('active'),('closed')) s(status)
WHERE i.tenant_id=7 AND i.name='orders';
INSERT INTO meta.lineage_service_dependencies(tenant_id,source_item_id,service_id,published_revision,dependency_kind,first_observed_at,last_observed_at,updated_at)
SELECT 7,id,77,'r1','read',now(),now(),'2001-01-01' FROM meta.meta_item WHERE tenant_id=7 AND name='orders';
ALTER TABLE scan_source.orders ADD COLUMN modified_at timestamptz;
SQL
(cd "$root/meta/backend" && ADDP_TEST_META_RESCAN=1 ADDP_TEST_PG_FACT_AUTHORITY=1 GOWORK=off go test ./internal/scanruntime -run '^TestPostgresWorkerScanFactAuthority$' -count=1 -v) | tee "$work/rescan.log"
grep -q '^--- PASS: TestPostgresWorkerScanFactAuthority ' "$work/rescan.log"
[[ "$(dba -c "SELECT count(*) FROM meta.lineage_item_relations WHERE status='stale' AND updated_at>'2001-01-01' AND source_field_name='label' AND target_field_name='id'")" == 1 ]]
[[ "$(dba -c "SELECT count(*) FROM meta.lineage_item_relations WHERE status='closed' AND updated_at='2001-01-01'")" == 1 ]]
[[ "$(dba -c "SELECT count(*) FROM meta.lineage_service_dependencies WHERE service_id=77 AND status='active' AND updated_at='2001-01-01'")" == 1 ]]
docker exec -e PGPASSWORD="$ADDP_TEST_META_WORKER_PASSWORD" "$name" psql -h127.0.0.1 -U meta_gate_worker -d addp_test -X -v ON_ERROR_STOP=1 -c "UPDATE meta.meta_item SET deleted_at=now() WHERE tenant_id=7 AND name='orders'"
[[ "$(dba -c "SELECT count(*) FROM meta.lineage_service_dependencies WHERE service_id=77 AND status='stale' AND updated_at>'2001-01-01'")" == 1 ]]
(cd "$root/meta/backend" && GOWORK=off go build -o "$work/meta-worker" ./cmd/worker)
export ADDP_DATABASE_RUNTIME_USER=meta_gate_worker ADDP_DATABASE_RUNTIME_PASSWORD="$ADDP_TEST_META_WORKER_PASSWORD"
(cd "$root/meta/backend" && ADDP_TEST_LINEAGE_CONCURRENCY=1 GOWORK=off go test ./internal/service -run '^TestLineageProjectionConcurrency$' -count=1 -timeout=60s -v) | tee "$work/lineage-concurrency.log"
grep -q '^--- PASS: TestLineageProjectionConcurrency ' "$work/lineage-concurrency.log"
dba <<'SQL'
CREATE TABLE public.ddl_seen(command text);
CREATE FUNCTION public.note_ddl() RETURNS event_trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN INSERT INTO public.ddl_seen VALUES(tg_tag); END $$;
CREATE EVENT TRIGGER meta_gate_ddl ON ddl_command_end EXECUTE FUNCTION public.note_ddl();
SQL
(cd "$root/system/backend" && ADDP_TEST_HEARTBEAT_RETENTION=1 GOWORK=off go test ./internal/service -run '^TestSystemRuntimeHeartbeatRetention$' -count=1 -v) | tee "$work/retention.log"
grep -q '^--- PASS: TestSystemRuntimeHeartbeatRetention ' "$work/retention.log"
dba -c 'TRUNCATE public.ddl_seen'
restart_status=0
python3 "$root/scripts/test/meta-database-restart-contract.py" "$work" postgres || restart_status=$?
[[ "$(dba -c 'SELECT count(*) FROM public.ddl_seen')" == 0 ]]
[[ "$(dba -c "SELECT count(*) FROM common.background_runtime_heartbeats WHERE module='meta' AND runtime_name='scan' AND stopped_at IS NOT NULL")" == 2 ]]
echo 'PG: no successful schema change during two runtime starts and stops'
test "$restart_status" = 0
dba -c 'DELETE FROM meta.startup_schema_revision'
python3 - "$work" <<'PY'
from pathlib import Path
import os
import subprocess
import sys

work = Path(sys.argv[1])
env = {key: os.environ[key] for key in ("POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_DB", "ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD")}
env.update(PROJECT_ROOT=str(work), ADDP_INFRA_DATABASE_PROVIDER="postgres", TZ="Asia/Shanghai",
           META_BACKEND_PORT="0", META_SERVICE_CLIENT_SECRET="x"*32, SYSTEM_URL="http://127.0.0.1:1")
for binary in ("meta-server", "meta-worker"):
    run = subprocess.run([work/binary], cwd=work, env=env, capture_output=True, text=True, timeout=20)
    expected = "schema meta version mismatch: database=0"
    if run.returncode != 1 or expected not in run.stdout + run.stderr:
        raise SystemExit(f"{binary}: exit {run.returncode}, missing expected revision error")
    print(f"{binary}: exit {run.returncode}, {expected}")
PY
[[ "$(dba -c 'SELECT count(*) FROM meta.startup_schema_revision')" == 0 ]]
