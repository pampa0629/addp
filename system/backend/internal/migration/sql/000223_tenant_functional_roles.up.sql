BEGIN;

-- Keep a complete immutable record before retiring organization-scoped functional
-- assignments. Organization identities/memberships and source-data grants are untouched.
LOCK TABLE system.role_assignments, system.roles, system.permissions IN SHARE ROW EXCLUSIVE MODE;
SET CONSTRAINTS ALL IMMEDIATE;
CREATE TEMP TABLE retired_functional_principals ON COMMIT DROP AS
SELECT DISTINCT a.principal_id FROM system.role_assignments a
JOIN system.roles r ON r.id=a.role_id
WHERE a.scope_type IN ('department','project_group')
   OR r.allowed_scope_types && ARRAY['department','project_group']::text[]
   OR EXISTS(SELECT 1 FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id
             WHERE rp.role_id=r.id AND p.allowed_scope_types && ARRAY['department','project_group']::text[]);

INSERT INTO system.audit_logs(context_type,tenant_id,event_name,result,risk_level,module_name,entity_type,entity_id,details)
SELECT 'tenant',a.tenant_id,'iam.tenant_role_assignment.retired','succeeded','high','system','role_assignment',a.id::text,
       jsonb_build_object('reason','organization_functional_scope_retired','assignment',to_jsonb(a))
FROM system.role_assignments a WHERE a.scope_type IN ('department','project_group');

ALTER TABLE system.role_assignments DISABLE TRIGGER trg_role_assignments_prevent_delete;
DELETE FROM system.role_assignments WHERE scope_type IN ('department','project_group');
ALTER TABLE system.role_assignments ENABLE TRIGGER trg_role_assignments_prevent_delete;

-- A role that never supported Tenant is disabled, never promoted into an active Tenant role.
INSERT INTO system.audit_logs(context_type,tenant_id,event_name,result,risk_level,module_name,entity_type,entity_id,details)
SELECT CASE WHEN r.tenant_id IS NULL THEN NULL ELSE 'tenant' END,r.tenant_id,'iam.tenant_role.retired','succeeded','high','system','role',r.id::text,
       jsonb_build_object('reason','organization_only_functional_role_retired','role',to_jsonb(r))
FROM system.roles r WHERE r.role_type<>'platform_builtin' AND NOT ('tenant'=ANY(r.allowed_scope_types));
UPDATE system.roles SET status='disabled',allowed_scope_types=ARRAY['tenant']::text[]
WHERE role_type<>'platform_builtin' AND NOT ('tenant'=ANY(allowed_scope_types));
UPDATE system.roles SET allowed_scope_types=array_remove(array_remove(allowed_scope_types,'department'),'project_group')
WHERE allowed_scope_types && ARRAY['department','project_group']::text[];
UPDATE system.permissions SET allowed_scope_types=array_remove(array_remove(allowed_scope_types,'department'),'project_group')
WHERE allowed_scope_types && ARRAY['department','project_group']::text[];

CREATE OR REPLACE FUNCTION system.valid_scope_types(values_to_check text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
 SELECT cardinality(values_to_check)>0 AND values_to_check <@ ARRAY['platform','tenant']::text[]
 AND cardinality(values_to_check)=(SELECT count(DISTINCT value)::integer FROM unnest(values_to_check) AS value);
$$;

ALTER TABLE system.role_assignments DROP COLUMN department_id, DROP COLUMN project_group_id;
ALTER TABLE system.role_assignments DROP CONSTRAINT role_assignments_scope_type_check;
ALTER TABLE system.role_assignments ADD CONSTRAINT role_assignments_scope_type_check CHECK(scope_type IN ('platform','tenant'));
ALTER TABLE system.role_assignments ADD CONSTRAINT role_assignments_context_scope_check
CHECK((scope_type='platform' AND tenant_id IS NULL) OR (scope_type='tenant' AND tenant_id IS NOT NULL));

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
               OR target_request.change_type NOT IN ('platform_role_grant','platform_role_replace')
               OR (CASE WHEN target_request.change_type='platform_role_replace' THEN target_request.successor_principal_id ELSE target_request.target_principal_id END) <> NEW.principal_id
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
               OR target_request.change_type NOT IN ('platform_role_revoke','platform_role_replace')
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

CREATE OR REPLACE FUNCTION system.guard_role_assignment_lifecycle()
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
                    COALESCE(NEW.tenant_id::text, '-')
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

UPDATE system.refresh_token_families SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
WHERE revoked_at IS NULL AND principal_id IN (SELECT principal_id FROM retired_functional_principals);
COMMIT;
