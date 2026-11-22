-- Run inside the Meta installation transaction after the fact tables and triggers exist.
-- The deployment sets addp.meta_trigger_owner locally to its dedicated NOLOGIN role.
DO $prepare$
DECLARE
    role_name text := current_setting('addp.meta_trigger_owner');
    owner_id oid;
    fn regprocedure;
BEGIN
    SELECT oid INTO STRICT owner_id FROM pg_catalog.pg_roles
    WHERE rolname = role_name AND NOT (rolcanlogin OR rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls);
    IF role_name = current_user OR EXISTS (SELECT FROM pg_catalog.pg_auth_members WHERE member=owner_id)
       OR pg_catalog.has_schema_privilege(owner_id, 'meta', 'CREATE') THEN
        RAISE EXCEPTION 'Meta requires a dedicated trigger owner without schema CREATE or inherited roles';
    END IF;

    IF EXISTS (
        WITH expected(rel, name, fn, kind) AS (VALUES
            ('meta.meta_node'::regclass, 'authorization_basis_node_membership', 'meta.advance_authorization_basis_revision()'::regprocedure, 13),
            ('meta.meta_node'::regclass, 'authorization_basis_node_update', 'meta.advance_authorization_basis_revision()'::regprocedure, 17),
            ('meta.meta_item'::regclass, 'authorization_basis_item_membership', 'meta.advance_authorization_basis_revision()'::regprocedure, 13),
            ('meta.meta_item'::regclass, 'authorization_basis_item_update', 'meta.advance_authorization_basis_revision()'::regprocedure, 17),
            ('meta.meta_item'::regclass, 'trg_meta_item_stale_field_lineage', 'meta.stale_lineage_for_changed_fields()'::regprocedure, 17),
            ('meta.meta_item'::regclass, 'trg_meta_item_stale_lineage', 'meta.stale_lineage_for_soft_deleted_item()'::regprocedure, 17),
            ('meta.meta_item'::regclass, 'trg_meta_item_data_item_change', 'meta.capture_data_item_change()'::regprocedure, 29)
        ), actual AS (
            SELECT tgrelid, tgname::text, tgfoid, tgtype, tgenabled
            FROM pg_catalog.pg_trigger WHERE tgfoid IN (SELECT expected.fn FROM expected)
        )
        SELECT FROM expected e FULL JOIN actual a
          ON (e.rel,e.name,e.fn,e.kind)=(a.tgrelid,a.tgname,a.tgfoid,a.tgtype)
        WHERE e.rel IS NULL OR a.tgrelid IS NULL OR a.tgenabled <> 'O'
    ) THEN
        RAISE EXCEPTION 'Meta fact trigger bindings do not match the installed schema';
    END IF;

    EXECUTE format('GRANT USAGE, CREATE ON SCHEMA meta TO %I',role_name);
    EXECUTE format('GRANT SELECT (tenant_id,version), INSERT (tenant_id,version), UPDATE (version) ON meta.authorization_basis_revisions TO %I',role_name);
    EXECUTE format('GRANT INSERT (tenant_id,item_id,source_identity,operation,snapshot,observed_at) ON meta.data_item_changes TO %I',role_name);
    EXECUTE format('GRANT USAGE ON SEQUENCE %s TO %I',pg_catalog.pg_get_serial_sequence('meta.data_item_changes','id')::regclass,role_name);

    EXECUTE format('GRANT SELECT (tenant_id,source_item_id,target_item_id,granularity,status), UPDATE (status,updated_at) ON meta.lineage_item_relations TO %I',role_name);
    EXECUTE format('GRANT SELECT (tenant_id,source_item_id,status), UPDATE (status,updated_at) ON meta.lineage_service_dependencies TO %I',role_name);

    FOREACH fn IN ARRAY ARRAY['meta.advance_authorization_basis_revision()'::regprocedure, 'meta.capture_data_item_change()'::regprocedure, 'meta.stale_lineage_for_changed_fields()'::regprocedure, 'meta.stale_lineage_for_soft_deleted_item()'::regprocedure] LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC',fn);
        IF EXISTS (
            SELECT FROM pg_catalog.pg_proc p, LATERAL pg_catalog.aclexplode(p.proacl) a
            WHERE p.oid=fn AND a.grantee<>p.proowner AND a.privilege_type='EXECUTE'
        ) THEN
            RAISE EXCEPTION 'Unexpected EXECUTE grant on Meta trigger function %', fn;
        END IF;
        EXECUTE format('ALTER FUNCTION %s OWNER TO %I',fn,role_name);
        EXECUTE format('ALTER FUNCTION %s SET search_path = pg_catalog, pg_temp',fn);
        EXECUTE format('ALTER FUNCTION %s SECURITY DEFINER',fn);
    END LOOP;
    EXECUTE format('REVOKE CREATE ON SCHEMA meta FROM %I',role_name);
END
$prepare$;
