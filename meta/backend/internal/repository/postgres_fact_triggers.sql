-- A single deployment owner manages Meta objects; triggers execute as the runtime caller.
DO $prepare$
DECLARE
    fn regprocedure;
BEGIN
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

    FOREACH fn IN ARRAY ARRAY['meta.advance_authorization_basis_revision()'::regprocedure, 'meta.capture_data_item_change()'::regprocedure, 'meta.stale_lineage_for_changed_fields()'::regprocedure, 'meta.stale_lineage_for_soft_deleted_item()'::regprocedure] LOOP
        IF EXISTS (SELECT FROM pg_catalog.pg_proc WHERE oid = fn
                   AND pg_catalog.pg_get_userbyid(proowner) = current_user AND NOT prosecdef
                   AND proconfig @> ARRAY['search_path=pg_catalog, pg_temp']) THEN
            CONTINUE;
        END IF;
        EXECUTE format('ALTER FUNCTION %s OWNER TO CURRENT_USER', fn);
        EXECUTE format('ALTER FUNCTION %s SECURITY INVOKER',fn);
        EXECUTE format('ALTER FUNCTION %s SET search_path = pg_catalog, pg_temp',fn);
    END LOOP;
END
$prepare$;
