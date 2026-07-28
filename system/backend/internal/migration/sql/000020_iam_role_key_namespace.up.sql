BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM system.roles builtin
        JOIN system.roles custom
          ON custom.role_key = builtin.role_key
         AND custom.tenant_id IS NOT NULL
        WHERE builtin.tenant_id IS NULL
    ) THEN
        RAISE EXCEPTION 'tenant custom role key conflicts with a built-in role key'
            USING ERRCODE = '23505';
    END IF;
END;
$$;

CREATE FUNCTION system.enforce_role_key_namespace()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.role_key, 0));

    IF NEW.tenant_id IS NULL AND EXISTS (
        SELECT 1
        FROM system.roles role
        WHERE role.tenant_id IS NOT NULL
          AND role.role_key = NEW.role_key
          AND role.id <> NEW.id
    ) THEN
        RAISE EXCEPTION 'built-in role key conflicts with a tenant custom role key'
            USING ERRCODE = '23505', CONSTRAINT = 'roles_role_key_namespace_unique';
    END IF;

    IF NEW.tenant_id IS NOT NULL AND EXISTS (
        SELECT 1
        FROM system.roles role
        WHERE role.tenant_id IS NULL
          AND role.role_key = NEW.role_key
          AND role.id <> NEW.id
    ) THEN
        RAISE EXCEPTION 'tenant custom role key conflicts with a built-in role key'
            USING ERRCODE = '23505', CONSTRAINT = 'roles_role_key_namespace_unique';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_roles_enforce_key_namespace
BEFORE INSERT OR UPDATE OF tenant_id, role_key ON system.roles
FOR EACH ROW EXECUTE FUNCTION system.enforce_role_key_namespace();

COMMIT;
