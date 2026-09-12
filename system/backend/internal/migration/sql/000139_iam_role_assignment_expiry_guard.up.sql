BEGIN;

-- A persisted active status means "not manually revoked", not "currently
-- effective". The old partial unique index therefore kept an expired history
-- row from ever being assigned again. Serialize the exact assignment identity
-- and reject only overlapping active validity intervals instead.
DROP INDEX system.uq_role_assignments_active_scope;

CREATE FUNCTION system.guard_role_assignment_lifecycle()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM pg_advisory_xact_lock(
            hashtextextended(
                concat_ws(
                    '|',
                    NEW.principal_id::text,
                    NEW.role_id::text,
                    NEW.scope_type,
                    COALESCE(NEW.tenant_id::text, '-'),
                    COALESCE(NEW.department_id::text, '-'),
                    COALESCE(NEW.project_group_id::text, '-')
                ),
                0
            )
        );

        IF NEW.status = 'active' AND EXISTS (
            SELECT 1
            FROM system.role_assignments AS existing
            WHERE existing.principal_id = NEW.principal_id
              AND existing.role_id = NEW.role_id
              AND existing.scope_type = NEW.scope_type
              AND existing.tenant_id IS NOT DISTINCT FROM NEW.tenant_id
              AND existing.department_id IS NOT DISTINCT FROM NEW.department_id
              AND existing.project_group_id IS NOT DISTINCT FROM NEW.project_group_id
              AND existing.status = 'active'
              AND tstzrange(existing.valid_from, existing.valid_until, '[)')
                  && tstzrange(NEW.valid_from, NEW.valid_until, '[)')
        ) THEN
            RAISE EXCEPTION 'role assignment validity interval overlaps an existing assignment'
                USING ERRCODE = '23505',
                      CONSTRAINT = 'role_assignments_effective_interval_overlap';
        END IF;
    ELSIF OLD.status = 'active'
          AND NEW.status = 'revoked'
          AND OLD.valid_until IS NOT NULL
          AND OLD.valid_until <= now() THEN
        RAISE EXCEPTION 'expired role assignment is read-only'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'role_assignments_expired_read_only';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_role_assignments_lifecycle_guard
BEFORE INSERT OR UPDATE ON system.role_assignments
FOR EACH ROW EXECUTE FUNCTION system.guard_role_assignment_lifecycle();

COMMIT;
