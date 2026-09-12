BEGIN;

-- The existing validation trigger still reads NEW.reason. Keep every trigger
-- disabled only inside this transaction while the column and lifecycle facts
-- are rewritten, then restore them after the replacement function is ready.
ALTER TABLE system.role_assignments DISABLE TRIGGER USER;

ALTER TABLE system.role_assignments
    RENAME COLUMN reason TO grant_reason;

ALTER TABLE system.role_assignments
    ALTER COLUMN grant_reason DROP NOT NULL,
    ALTER COLUMN grant_reason DROP DEFAULT,
    ADD COLUMN revoked_reason text;

UPDATE system.role_assignments
SET grant_reason = NULL
WHERE btrim(grant_reason) = '';

UPDATE system.role_assignments AS assignment
SET revoked_reason = COALESCE(
    (
        SELECT NULLIF(btrim(audit.details ->> 'reason'), '')
        FROM system.audit_logs AS audit
        WHERE audit.entity_type = 'role_assignment'
          AND audit.entity_id = assignment.id::text
          AND audit.event_name = 'iam.tenant_role_assignment.revoked'
          AND audit.result = 'succeeded'
          AND audit.tenant_id IS NOT DISTINCT FROM assignment.tenant_id
          AND NULLIF(btrim(audit.details ->> 'reason'), '') IS NOT NULL
        ORDER BY audit.created_at DESC, audit.id DESC
        LIMIT 1
    ),
    (
        SELECT NULLIF(btrim(request.reason), '')
        FROM system.privileged_change_requests AS request
        WHERE request.id = assignment.revoke_change_request_id
    ),
    (
        SELECT NULLIF(btrim(audit.details ->> 'reason'), '')
        FROM system.audit_logs AS audit
        WHERE audit.entity_type = 'role'
          AND audit.entity_id = assignment.role_id::text
          AND audit.event_name = 'iam.tenant_role.deleted'
          AND audit.result = 'succeeded'
          AND audit.tenant_id IS NOT DISTINCT FROM assignment.tenant_id
          AND NULLIF(btrim(audit.details ->> 'reason'), '') IS NOT NULL
        ORDER BY audit.created_at DESC, audit.id DESC
        LIMIT 1
    ),
    CASE
        WHEN EXISTS (
            SELECT 1
            FROM system.roles AS role
            JOIN system.service_principals AS service_principal
              ON service_principal.id = assignment.principal_id
            WHERE role.id = assignment.role_id
              AND role.role_key = 'tenant.portal_runtime'
              AND service_principal.name = 'addp-portal'
        ) THEN 'platform runtime role retired by migration 000085'
        ELSE NULL
    END
)
WHERE assignment.status = 'revoked';

DO $migration$
DECLARE
    missing_ids text;
    lifecycle_constraint_name text;
BEGIN
    SELECT string_agg(id::text, ', ' ORDER BY id)
    INTO missing_ids
    FROM system.role_assignments
    WHERE status = 'revoked'
      AND NULLIF(btrim(revoked_reason), '') IS NULL;

    IF missing_ids IS NOT NULL THEN
        RAISE EXCEPTION 'cannot migrate revoked role assignments without an authoritative reason: %', missing_ids;
    END IF;

    SELECT constraint_definition.conname
    INTO lifecycle_constraint_name
    FROM pg_constraint AS constraint_definition
    WHERE constraint_definition.conrelid = 'system.role_assignments'::regclass
      AND constraint_definition.contype = 'c'
      AND pg_get_constraintdef(constraint_definition.oid) LIKE '%revoked_by_principal_id%'
      AND pg_get_constraintdef(constraint_definition.oid) LIKE '%revoke_change_request_id%'
    LIMIT 1;

    IF lifecycle_constraint_name IS NULL THEN
        RAISE EXCEPTION 'role assignment lifecycle constraint was not found';
    END IF;

    EXECUTE format(
        'ALTER TABLE system.role_assignments DROP CONSTRAINT %I',
        lifecycle_constraint_name
    );
END
$migration$;

ALTER TABLE system.role_assignments
    ADD CONSTRAINT role_assignments_grant_reason_check
        CHECK (grant_reason IS NULL OR btrim(grant_reason) <> ''),
    ADD CONSTRAINT role_assignments_lifecycle_check
        CHECK (
            (status = 'active'
                AND revoked_by_principal_id IS NULL
                AND revoked_at IS NULL
                AND revoked_reason IS NULL
                AND revoke_change_request_id IS NULL)
            OR
            (status = 'revoked'
                AND revoked_by_principal_id IS NOT NULL
                AND revoked_at IS NOT NULL
                AND revoked_reason IS NOT NULL
                AND btrim(revoked_reason) <> '')
        );

CREATE OR REPLACE FUNCTION system.validate_role_assignment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_principal system.principals%ROWTYPE;
    target_role system.roles%ROWTYPE;
    target_request system.privileged_change_requests%ROWTYPE;
BEGIN
    SELECT * INTO target_principal
    FROM system.principals
    WHERE id = NEW.principal_id
    FOR UPDATE;
    SELECT * INTO target_role
    FROM system.roles
    WHERE id = NEW.role_id
    FOR KEY SHARE;

    IF TG_OP = 'UPDATE' THEN
        IF OLD.status = 'revoked' THEN
            RAISE EXCEPTION 'revoked role assignment is immutable'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.principal_id <> OLD.principal_id
           OR NEW.role_id <> OLD.role_id
           OR NEW.scope_type <> OLD.scope_type
           OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
           OR NEW.department_id IS DISTINCT FROM OLD.department_id
           OR NEW.project_group_id IS DISTINCT FROM OLD.project_group_id
           OR NEW.source_type <> OLD.source_type
           OR NEW.created_by_principal_id IS DISTINCT FROM OLD.created_by_principal_id
           OR NEW.grant_change_request_id IS DISTINCT FROM OLD.grant_change_request_id
           OR NEW.valid_from <> OLD.valid_from
           OR NEW.valid_until IS DISTINCT FROM OLD.valid_until
           OR NEW.grant_reason IS DISTINCT FROM OLD.grant_reason
           OR NEW.created_at <> OLD.created_at THEN
            RAISE EXCEPTION 'role assignment identity cannot change'
                USING ERRCODE = '23514';
        END IF;
        IF OLD.status = 'active' AND NEW.status <> 'revoked' THEN
            RAISE EXCEPTION 'role assignment status may only move from active to revoked'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.status <> 'active' THEN
        RAISE EXCEPTION 'new role assignment must be active'
            USING ERRCODE = '23514';
	ELSIF NEW.source_type <> 'bootstrap' AND NULLIF(btrim(NEW.grant_reason), '') IS NULL THEN
		RAISE EXCEPTION 'new role assignment requires a grant reason'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.status = 'active' THEN
        IF target_principal.status <> 'active' OR target_role.status <> 'active' THEN
            RAISE EXCEPTION 'active role assignment requires an active principal and role'
                USING ERRCODE = '23514';
        END IF;
        IF NOT (target_principal.principal_type = ANY(target_role.allowed_principal_types))
           OR NOT (NEW.scope_type = ANY(target_role.allowed_scope_types)) THEN
            RAISE EXCEPTION 'role assignment principal type or scope is not allowed by the role'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.scope_type = 'platform' AND target_role.role_type <> 'platform_builtin' THEN
            RAISE EXCEPTION 'platform scope requires a platform built-in role'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.scope_type <> 'platform' AND target_role.role_type = 'platform_builtin' THEN
            RAISE EXCEPTION 'platform built-in role only supports platform scope'
                USING ERRCODE = '23514';
        END IF;
        IF target_role.role_type = 'tenant_custom' AND target_role.tenant_id <> NEW.tenant_id THEN
            RAISE EXCEPTION 'tenant custom role may only be assigned in its own tenant'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.scope_type <> 'platform' AND NOT EXISTS (
            SELECT 1
            FROM system.tenant_memberships tm
            WHERE tm.tenant_id = NEW.tenant_id
              AND tm.principal_id = NEW.principal_id
              AND tm.status = 'active'
              AND (tm.expires_at IS NULL OR tm.expires_at > now())
        ) THEN
            RAISE EXCEPTION 'non-platform role assignment requires an active tenant membership'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.scope_type = 'department' AND NOT EXISTS (
            SELECT 1
            FROM system.department_memberships dm
            JOIN system.tenant_memberships tm ON tm.id = dm.tenant_membership_id
            JOIN system.departments d ON d.id = dm.department_id
            WHERE dm.tenant_id = NEW.tenant_id
              AND dm.department_id = NEW.department_id
              AND tm.principal_id = NEW.principal_id
              AND dm.status = 'active'
              AND d.status = 'active'
        ) THEN
            RAISE EXCEPTION 'department role assignment requires an active department membership'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.scope_type = 'project_group' AND NOT EXISTS (
            SELECT 1
            FROM system.project_group_memberships pgm
            JOIN system.tenant_memberships tm ON tm.id = pgm.tenant_membership_id
            JOIN system.project_groups pg ON pg.id = pgm.project_group_id
            WHERE pgm.tenant_id = NEW.tenant_id
              AND pgm.project_group_id = NEW.project_group_id
              AND tm.principal_id = NEW.principal_id
              AND pgm.status = 'active'
              AND pg.status <> 'closed'
        ) THEN
            RAISE EXCEPTION 'project group role assignment requires an active project group membership'
                USING ERRCODE = '23514';
        END IF;

        IF TG_OP = 'INSERT' AND EXISTS (
            SELECT 1
            FROM system.role_assignments existing
            JOIN system.role_conflicts conflict
              ON (conflict.role_id_low = LEAST(existing.role_id, NEW.role_id)
                  AND conflict.role_id_high = GREATEST(existing.role_id, NEW.role_id))
            WHERE existing.principal_id = NEW.principal_id
              AND existing.status = 'active'
              AND (existing.valid_until IS NULL OR existing.valid_until > now())
        ) THEN
            RAISE EXCEPTION 'principal already has a conflicting active role'
                USING ERRCODE = '23514';
        END IF;

        IF NEW.scope_type = 'platform' AND NEW.source_type <> 'bootstrap' THEN
            IF NEW.grant_change_request_id IS NULL THEN
                RAISE EXCEPTION 'platform role assignment requires an approved grant request'
                    USING ERRCODE = '23514';
            END IF;
            SELECT * INTO target_request
            FROM system.privileged_change_requests
            WHERE id = NEW.grant_change_request_id
            FOR UPDATE;
            IF target_request.status <> 'approved'
               OR target_request.change_type <> 'platform_role_grant'
               OR target_request.target_principal_id <> NEW.principal_id
               OR target_request.target_role_id <> NEW.role_id
               OR target_request.requested_by_principal_id <> NEW.created_by_principal_id THEN
                RAISE EXCEPTION 'platform role grant request does not match the assignment'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF NEW.grant_change_request_id IS NOT NULL THEN
            RAISE EXCEPTION 'grant request is only valid for non-bootstrap platform assignments'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.status = 'revoked' THEN
        IF NULLIF(btrim(NEW.revoked_reason), '') IS NULL THEN
            RAISE EXCEPTION 'revoked role assignment requires a revocation reason'
                USING ERRCODE = '23514';
        END IF;
        IF NEW.scope_type = 'platform' THEN
            IF NEW.revoke_change_request_id IS NULL THEN
                RAISE EXCEPTION 'platform role revocation requires an approved revoke request'
                    USING ERRCODE = '23514';
            END IF;
            SELECT * INTO target_request
            FROM system.privileged_change_requests
            WHERE id = NEW.revoke_change_request_id
            FOR UPDATE;
            IF target_request.status <> 'approved'
               OR target_request.change_type <> 'platform_role_revoke'
               OR target_request.target_principal_id <> NEW.principal_id
               OR target_request.target_role_id <> NEW.role_id
               OR target_request.requested_by_principal_id <> NEW.revoked_by_principal_id THEN
                RAISE EXCEPTION 'platform role revoke request does not match the assignment'
                    USING ERRCODE = '23514';
            END IF;
        ELSIF NEW.revoke_change_request_id IS NOT NULL THEN
            RAISE EXCEPTION 'revoke request is only valid for platform assignments'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE system.role_assignments ENABLE TRIGGER USER;

COMMIT;
