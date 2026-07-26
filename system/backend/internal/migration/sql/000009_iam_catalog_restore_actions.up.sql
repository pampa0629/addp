BEGIN;

ALTER TABLE system.privileged_change_requests
    DROP CONSTRAINT privileged_change_requests_change_type_check,
    DROP CONSTRAINT privileged_change_requests_check1;

ALTER TABLE system.privileged_change_requests
    ADD CONSTRAINT privileged_change_requests_change_type_check CHECK (change_type IN (
        'platform_role_grant',
        'platform_role_revoke',
        'platform_identity_suspend',
        'platform_identity_reactivate',
        'platform_identity_deactivate'
    )),
    ADD CONSTRAINT privileged_change_requests_target_check CHECK (
        (change_type IN ('platform_role_grant', 'platform_role_revoke') AND target_role_id IS NOT NULL)
        OR (change_type IN (
            'platform_identity_suspend',
            'platform_identity_reactivate',
            'platform_identity_deactivate'
        ) AND target_role_id IS NULL)
    );

CREATE OR REPLACE FUNCTION system.validate_privileged_change_request()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_type text;
    requester_type text;
    target_status text;
    requester_status text;
    target_role system.roles%ROWTYPE;
BEGIN
    PERFORM 1
    FROM system.principals
    WHERE id IN (NEW.target_principal_id, NEW.requested_by_principal_id)
    ORDER BY id
    FOR KEY SHARE;

    SELECT principal_type, status INTO target_type, target_status
    FROM system.principals
    WHERE id = NEW.target_principal_id;
    SELECT principal_type, status INTO requester_type, requester_status
    FROM system.principals
    WHERE id = NEW.requested_by_principal_id;

    IF NEW.status <> 'pending' OR NEW.decided_at IS NOT NULL OR NEW.applied_at IS NOT NULL THEN
        RAISE EXCEPTION 'new privileged change request must be pending'
            USING ERRCODE = '23514';
    END IF;
    IF target_type <> 'user' OR requester_type <> 'user' OR requester_status <> 'active' THEN
        RAISE EXCEPTION 'privileged change target and requester must be users'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.target_role_id IS NOT NULL THEN
        SELECT * INTO target_role FROM system.roles WHERE id = NEW.target_role_id FOR KEY SHARE;
        IF target_role.role_type <> 'platform_builtin'
           OR target_role.status <> 'active'
           OR target_role.allowed_scope_types <> ARRAY['platform']::text[] THEN
            RAISE EXCEPTION 'platform role change requires an active platform built-in role'
                USING ERRCODE = '23514';
        END IF;
    END IF;

    IF NEW.change_type = 'platform_role_grant' AND (
        target_status <> 'active' OR EXISTS (
            SELECT 1
            FROM system.role_assignments assignment
            WHERE assignment.principal_id = NEW.target_principal_id
              AND assignment.role_id = NEW.target_role_id
              AND assignment.scope_type = 'platform'
              AND assignment.status = 'active'
        )
    ) THEN
        RAISE EXCEPTION 'platform role grant requires an active target without the role'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.change_type = 'platform_role_revoke' AND NOT EXISTS (
        SELECT 1
        FROM system.role_assignments assignment
        WHERE assignment.principal_id = NEW.target_principal_id
          AND assignment.role_id = NEW.target_role_id
          AND assignment.scope_type = 'platform'
          AND assignment.status = 'active'
          AND assignment.valid_from <= now()
          AND (assignment.valid_until IS NULL OR assignment.valid_until > now())
    ) THEN
        RAISE EXCEPTION 'platform role revoke requires an effective target assignment'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.change_type = 'platform_identity_suspend' AND target_status <> 'active' THEN
        RAISE EXCEPTION 'platform identity suspend requires an active target'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.change_type = 'platform_identity_reactivate' AND target_status <> 'suspended' THEN
        RAISE EXCEPTION 'platform identity reactivate requires a suspended target'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.change_type = 'platform_identity_deactivate' AND target_status = 'deactivated' THEN
        RAISE EXCEPTION 'platform identity deactivate requires a non-deactivated target'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.change_type IN (
        'platform_identity_suspend',
        'platform_identity_reactivate',
        'platform_identity_deactivate'
    ) AND NOT EXISTS (
        SELECT 1
        FROM system.role_assignments assignment
        WHERE assignment.principal_id = NEW.target_principal_id
          AND assignment.scope_type = 'platform'
          AND assignment.status = 'active'
          AND assignment.valid_from <= now()
          AND (assignment.valid_until IS NULL OR assignment.valid_until > now())
    ) THEN
        RAISE EXCEPTION 'platform identity change requires an effective platform role assignment'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.validate_privileged_change_request_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.change_type <> OLD.change_type
       OR NEW.target_principal_id <> OLD.target_principal_id
       OR NEW.target_role_id IS DISTINCT FROM OLD.target_role_id
       OR NEW.scope_type <> OLD.scope_type
       OR NEW.reason <> OLD.reason
       OR NEW.requested_by_principal_id <> OLD.requested_by_principal_id
       OR NEW.requested_at <> OLD.requested_at
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'privileged change request identity cannot change'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.status = OLD.status THEN
        IF NEW.decided_at IS DISTINCT FROM OLD.decided_at
           OR NEW.applied_at IS DISTINCT FROM OLD.applied_at THEN
            RAISE EXCEPTION 'privileged change decision timestamps cannot change without a status transition'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.status = 'pending' AND NEW.status = 'cancelled' THEN
        RETURN NEW;
    END IF;
    IF OLD.status = 'pending' AND NEW.status IN ('approved', 'rejected') AND EXISTS (
        SELECT 1
        FROM system.privileged_change_approvals approval
        WHERE approval.request_id = OLD.id
          AND approval.decision = NEW.status
    ) THEN
        RETURN NEW;
    END IF;
    IF OLD.status = 'approved' AND NEW.status = 'applied' AND (
        (OLD.change_type = 'platform_role_grant' AND EXISTS (
            SELECT 1 FROM system.role_assignments assignment
            WHERE assignment.grant_change_request_id = OLD.id
        ))
        OR (OLD.change_type = 'platform_role_revoke' AND EXISTS (
            SELECT 1 FROM system.role_assignments assignment
            WHERE assignment.revoke_change_request_id = OLD.id AND assignment.status = 'revoked'
        ))
        OR (OLD.change_type = 'platform_identity_suspend' AND EXISTS (
            SELECT 1 FROM system.principals principal
            WHERE principal.id = OLD.target_principal_id
              AND principal.status = 'suspended'
              AND principal.status_change_request_id = OLD.id
        ))
        OR (OLD.change_type = 'platform_identity_reactivate' AND EXISTS (
            SELECT 1 FROM system.principals principal
            WHERE principal.id = OLD.target_principal_id
              AND principal.status = 'active'
              AND principal.status_change_request_id = OLD.id
        ))
        OR (OLD.change_type = 'platform_identity_deactivate' AND EXISTS (
            SELECT 1 FROM system.principals principal
            WHERE principal.id = OLD.target_principal_id
              AND principal.status = 'deactivated'
              AND principal.status_change_request_id = OLD.id
        ))
    ) THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'invalid privileged change request status transition from % to %', OLD.status, NEW.status
        USING ERRCODE = '23514';
END;
$$;

CREATE OR REPLACE FUNCTION system.validate_principal_privileged_status_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    expected_change_type text;
    target_request system.privileged_change_requests%ROWTYPE;
    requires_approval boolean;
BEGIN
    IF NEW.status = OLD.status THEN
        IF NEW.status_change_request_id IS DISTINCT FROM OLD.status_change_request_id THEN
            RAISE EXCEPTION 'principal status request may only change with principal status'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.status = 'deactivated'
       OR NOT (
           (OLD.status = 'active' AND NEW.status IN ('suspended', 'deactivated'))
           OR (OLD.status = 'suspended' AND NEW.status IN ('active', 'deactivated'))
       ) THEN
        RAISE EXCEPTION 'invalid principal status transition from % to %', OLD.status, NEW.status
            USING ERRCODE = '23514';
    END IF;

    NEW.authorization_version := OLD.authorization_version + 1;
    SELECT EXISTS (
        SELECT 1
        FROM system.role_assignments assignment
        WHERE assignment.principal_id = OLD.id
          AND assignment.scope_type = 'platform'
          AND assignment.status = 'active'
          AND assignment.valid_from <= now()
          AND (assignment.valid_until IS NULL OR assignment.valid_until > now())
    ) INTO requires_approval;

    IF NEW.principal_type = 'user' AND requires_approval THEN
        expected_change_type := CASE
            WHEN NEW.status = 'suspended' THEN 'platform_identity_suspend'
            WHEN NEW.status = 'deactivated' THEN 'platform_identity_deactivate'
            ELSE 'platform_identity_reactivate'
        END;
        IF NEW.status_change_request_id IS NULL
           OR NEW.status_change_request_id IS NOT DISTINCT FROM OLD.status_change_request_id THEN
            RAISE EXCEPTION 'governed principal status change requires a new approved request'
                USING ERRCODE = '23514';
        END IF;
        SELECT * INTO target_request
        FROM system.privileged_change_requests
        WHERE id = NEW.status_change_request_id
        FOR UPDATE;
        IF target_request.status <> 'approved'
           OR target_request.change_type <> expected_change_type
           OR target_request.target_principal_id <> NEW.id THEN
            RAISE EXCEPTION 'identity change request does not match the principal status change'
                USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.status_change_request_id IS DISTINCT FROM OLD.status_change_request_id THEN
        RAISE EXCEPTION 'status change request is only valid for governed user status changes'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable, allowed_scope_types,
    tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES
    ('iam.tenant_membership.restore', 'system', 'restore', 'high', false, ARRAY['tenant']::text[], false, 'permissions.iam.tenant_membership.restore.name', 'permissions.iam.tenant_membership.restore.description', 'active'),
    ('iam.user.reactivate', 'system', 'reactivate', 'high', false, ARRAY['platform']::text[], false, 'permissions.iam.user.reactivate.name', 'permissions.iam.user.reactivate.description', 'active'),
    ('platform.tenant.restore', 'system', 'restore', 'high', false, ARRAY['platform']::text[], false, 'permissions.platform.tenant.restore.name', 'permissions.platform.tenant.restore.description', 'active');

INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
SELECT role.id, permission.id, 'product', NULL
FROM (VALUES
    ('platform.security_administrator', 'iam.user.reactivate'),
    ('platform.system_administrator', 'platform.tenant.restore'),
    ('tenant.administrator', 'iam.tenant_membership.restore')
) AS seed(role_key, permission_key)
JOIN system.roles role ON role.tenant_id IS NULL AND role.role_key = seed.role_key
JOIN system.permissions permission ON permission.permission_key = seed.permission_key
ORDER BY seed.role_key, seed.permission_key;

COMMIT;
