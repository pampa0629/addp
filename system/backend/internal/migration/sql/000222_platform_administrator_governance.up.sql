BEGIN;
-- Outstanding approvals cannot authorize actions under a different governance policy.
UPDATE system.privileged_change_requests SET status='cancelled',decided_at=statement_timestamp(),updated_at=statement_timestamp()
WHERE status='pending';
ALTER TABLE system.privileged_change_requests
 ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 ADD COLUMN governance_role_key text,
 ADD COLUMN target_authorization_version bigint,
 ADD COLUMN requester_authorization_version bigint,
 ADD COLUMN target_assignment_id bigint REFERENCES system.role_assignments(id),
 ADD COLUMN successor_principal_id bigint REFERENCES system.principals(id),
 ADD COLUMN successor_authorization_version bigint,
 ADD COLUMN expires_at timestamptz NOT NULL DEFAULT (statement_timestamp()+interval '24 hours');
ALTER TABLE system.privileged_change_requests DROP CONSTRAINT privileged_change_requests_change_type_check,
 DROP CONSTRAINT privileged_change_requests_target_check;
ALTER TABLE system.privileged_change_requests ADD CONSTRAINT privileged_change_requests_change_type_check
 CHECK(change_type IN('platform_role_grant','platform_role_revoke','platform_role_replace','platform_identity_suspend','platform_identity_reactivate','platform_identity_deactivate','platform_credential_recovery')),
 ADD CONSTRAINT privileged_change_requests_target_check CHECK(
 (change_type IN('platform_role_grant','platform_role_revoke','platform_role_replace') AND target_role_id IS NOT NULL)
 OR (change_type IN('platform_identity_suspend','platform_identity_reactivate','platform_identity_deactivate','platform_credential_recovery') AND target_role_id IS NULL)),
 ADD CONSTRAINT privileged_change_requests_successor_check CHECK((change_type='platform_role_replace')=(successor_principal_id IS NOT NULL));

CREATE INDEX idx_administrator_changes_assignment ON system.privileged_change_requests(target_assignment_id) WHERE target_assignment_id IS NOT NULL;
CREATE INDEX idx_administrator_changes_successor ON system.privileged_change_requests(successor_principal_id) WHERE successor_principal_id IS NOT NULL;

CREATE FUNCTION system.platform_administrator_role(subject bigint) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT r.role_key FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=subject AND a.scope_type='platform' AND a.status='active'
 AND a.valid_from<=statement_timestamp() AND (a.valid_until IS NULL OR a.valid_until>statement_timestamp())
 AND r.status='active' AND r.role_key IN('platform.system_administrator','platform.security_administrator','platform.audit_administrator')
 ORDER BY a.id LIMIT 1
$$;
CREATE FUNCTION system.administrator_change_requester(role_key text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE role_key WHEN 'platform.security_administrator' THEN 'platform.system_administrator'
 WHEN 'platform.system_administrator' THEN 'platform.security_administrator'
 WHEN 'platform.audit_administrator' THEN 'platform.security_administrator' END
$$;
CREATE FUNCTION system.administrator_change_reviewer(role_key text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE role_key WHEN 'platform.audit_administrator' THEN 'platform.system_administrator'
 WHEN 'platform.system_administrator' THEN 'platform.audit_administrator'
 WHEN 'platform.security_administrator' THEN 'platform.audit_administrator' END
$$;

CREATE OR REPLACE FUNCTION system.validate_privileged_change_request() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 target system.principals%ROWTYPE;
 requester system.principals%ROWTYPE;
 successor system.principals%ROWTYPE;
 expected_role text;
BEGIN
 PERFORM 1 FROM system.principals WHERE id IN(NEW.target_principal_id,NEW.requested_by_principal_id,NEW.successor_principal_id) ORDER BY id FOR UPDATE;
 SELECT * INTO target FROM system.principals WHERE id=NEW.target_principal_id;
 SELECT * INTO requester FROM system.principals WHERE id=NEW.requested_by_principal_id;
 IF NEW.status<>'pending' OR NEW.decided_at IS NOT NULL OR NEW.applied_at IS NOT NULL
 OR target.principal_type<>'user' OR requester.principal_type<>'user' OR requester.status<>'active'
 OR target.status='deactivated' THEN
  RAISE EXCEPTION 'invalid administrator change identities or initial state' USING ERRCODE='23514';
 END IF;
 IF NEW.target_role_id IS NOT NULL THEN
  SELECT role_key INTO expected_role FROM system.roles WHERE id=NEW.target_role_id AND status='active' AND role_type='platform_builtin';
 ELSE
  expected_role:=system.platform_administrator_role(target.id);
 END IF;
 IF system.administrator_change_requester(expected_role) IS NULL
 OR system.platform_administrator_role(requester.id) IS DISTINCT FROM system.administrator_change_requester(expected_role) THEN
  RAISE EXCEPTION 'administrator change requester is not eligible for target role' USING ERRCODE='42501';
 END IF;
 NEW.governance_role_key:=expected_role;
 NEW.target_authorization_version:=target.authorization_version;
 NEW.requester_authorization_version:=requester.authorization_version;
 NEW.target_assignment_id:=NULL;
 SELECT a.id INTO NEW.target_assignment_id FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=target.id AND r.role_key=expected_role AND a.scope_type='platform'
 AND a.status='active' AND a.valid_from<=statement_timestamp() AND (a.valid_until IS NULL OR a.valid_until>statement_timestamp()) ORDER BY a.id LIMIT 1;
 IF NEW.change_type='platform_role_grant' THEN
  IF target.status<>'active' OR system.platform_administrator_role(target.id) IS NOT NULL
  OR NOT EXISTS(SELECT 1 FROM system.mfa_credentials WHERE user_id=target.id AND status='active')
 OR NOT EXISTS(SELECT 1 FROM system.local_accounts WHERE user_id=target.id AND status='active') THEN
   RAISE EXCEPTION 'administrator candidate must be active, MFA enrolled and without a platform role' USING ERRCODE='23514';
  END IF;
 ELSIF NEW.target_assignment_id IS NULL THEN
  RAISE EXCEPTION 'administrator change requires an effective target assignment' USING ERRCODE='23514';
 END IF;
 IF NEW.change_type='platform_identity_suspend' AND target.status<>'active'
 OR NEW.change_type='platform_identity_reactivate' AND target.status<>'suspended' THEN
  RAISE EXCEPTION 'administrator identity state does not match requested action' USING ERRCODE='23514';
 END IF;
 IF NEW.change_type='platform_role_replace' THEN
  SELECT * INTO successor FROM system.principals WHERE id=NEW.successor_principal_id;
  IF successor.principal_type<>'user' OR successor.status<>'active'
  OR successor.id IN(target.id,requester.id)
  OR system.platform_administrator_role(successor.id) IS NOT NULL
  OR NOT EXISTS(SELECT 1 FROM system.mfa_credentials WHERE user_id=successor.id AND status='active')
 OR NOT EXISTS(SELECT 1 FROM system.local_accounts WHERE user_id=successor.id AND status='active') THEN
   RAISE EXCEPTION 'administrator successor must be a distinct active MFA enrolled candidate' USING ERRCODE='23514';
  END IF;
  NEW.successor_authorization_version:=successor.authorization_version;
 END IF;
 NEW.requested_at:=statement_timestamp();
 NEW.expires_at:=NEW.requested_at+interval '24 hours';
 NEW.version:=1;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.validate_privileged_change_approval() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE req system.privileged_change_requests%ROWTYPE;
BEGIN
 SELECT * INTO req FROM system.privileged_change_requests WHERE id=NEW.request_id FOR UPDATE;
 IF req.status<>'pending' OR req.governance_role_key IS NULL OR req.expires_at<=statement_timestamp()
 OR NEW.reviewer_principal_id IN(req.target_principal_id,req.requested_by_principal_id)
 OR NEW.reviewer_principal_id IS NOT DISTINCT FROM req.successor_principal_id
 OR system.platform_administrator_role(NEW.reviewer_principal_id) IS DISTINCT FROM system.administrator_change_reviewer(req.governance_role_key)
 OR system.platform_administrator_role(req.requested_by_principal_id) IS DISTINCT FROM system.administrator_change_requester(req.governance_role_key)
 OR NOT EXISTS(SELECT 1 FROM system.principals WHERE id=NEW.reviewer_principal_id AND principal_type='user' AND status='active')
 OR NOT EXISTS(SELECT 1 FROM system.principals WHERE id=req.requested_by_principal_id AND status='active' AND authorization_version=req.requester_authorization_version)
 OR NOT EXISTS(SELECT 1 FROM system.principals WHERE id=req.target_principal_id AND authorization_version=req.target_authorization_version)
 OR (req.target_assignment_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM system.role_assignments WHERE id=req.target_assignment_id AND status='active' AND valid_from<=statement_timestamp() AND (valid_until IS NULL OR valid_until>statement_timestamp())))
 OR (req.successor_principal_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM system.principals WHERE id=req.successor_principal_id AND status='active' AND authorization_version=req.successor_authorization_version)) THEN
  RAISE EXCEPTION 'administrator review is ineligible, expired or stale' USING ERRCODE='23514';
 END IF;
 NEW.decided_at:=statement_timestamp();
 RETURN NEW;
END;
$$;

CREATE TABLE system.administrator_credential_recoveries (
 request_id bigint PRIMARY KEY REFERENCES system.privileged_change_requests(id),
 ticket_hash text UNIQUE,
 enrollment_hash text UNIQUE,
 reviewer_principal_id bigint NOT NULL REFERENCES system.principals(id),
 reviewer_authorization_version bigint NOT NULL CHECK(reviewer_authorization_version>0),
 secret_ciphertext bytea,
 secret_nonce bytea,
 key_version integer NOT NULL DEFAULT 1,
 failed_attempts integer NOT NULL DEFAULT 0 CHECK(failed_attempts>=0),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 CHECK(ticket_hash IS NULL OR enrollment_hash IS NULL),
 CHECK(consumed_at IS NULL OR (ticket_hash IS NULL AND enrollment_hash IS NULL AND secret_ciphertext IS NULL AND secret_nonce IS NULL))
);

CREATE INDEX idx_administrator_recovery_reviewer ON system.administrator_credential_recoveries(reviewer_principal_id);

CREATE OR REPLACE FUNCTION system.validate_privileged_change_request_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.change_type,NEW.target_principal_id,NEW.target_role_id,NEW.scope_type,NEW.reason,NEW.requested_by_principal_id,NEW.requested_at,NEW.created_at,
 NEW.governance_role_key,NEW.target_authorization_version,NEW.requester_authorization_version,NEW.target_assignment_id,NEW.successor_principal_id,NEW.successor_authorization_version,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.change_type,OLD.target_principal_id,OLD.target_role_id,OLD.scope_type,OLD.reason,OLD.requested_by_principal_id,OLD.requested_at,OLD.created_at,
 OLD.governance_role_key,OLD.target_authorization_version,OLD.requester_authorization_version,OLD.target_assignment_id,OLD.successor_principal_id,OLD.successor_authorization_version,OLD.expires_at) THEN
  RAISE EXCEPTION 'administrator request facts are immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.status=OLD.status THEN
  IF ROW(NEW.version,NEW.decided_at,NEW.applied_at) IS DISTINCT FROM ROW(OLD.version,OLD.decided_at,OLD.applied_at) THEN
   RAISE EXCEPTION 'administrator request decision is immutable without transition' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 NEW.version:=OLD.version+1;
 IF OLD.status IN('pending','approved') AND NEW.status='cancelled' THEN RETURN NEW; END IF;
 IF OLD.status='pending' AND NEW.status IN('approved','rejected') AND EXISTS(
  SELECT 1 FROM system.privileged_change_approvals WHERE request_id=OLD.id AND decision=NEW.status) THEN RETURN NEW; END IF;
 IF OLD.status='approved' AND NEW.status='applied' AND (
  (OLD.change_type='platform_role_grant' AND EXISTS(SELECT 1 FROM system.role_assignments WHERE grant_change_request_id=OLD.id))
  OR (OLD.change_type='platform_role_revoke' AND EXISTS(SELECT 1 FROM system.role_assignments WHERE revoke_change_request_id=OLD.id AND status='revoked'))
  OR (OLD.change_type='platform_role_replace' AND EXISTS(SELECT 1 FROM system.role_assignments WHERE grant_change_request_id=OLD.id AND principal_id=OLD.successor_principal_id)
   AND EXISTS(SELECT 1 FROM system.role_assignments WHERE revoke_change_request_id=OLD.id AND id=OLD.target_assignment_id AND status='revoked'))
  OR (OLD.change_type='platform_credential_recovery' AND EXISTS(SELECT 1 FROM system.administrator_credential_recoveries WHERE request_id=OLD.id AND consumed_at IS NOT NULL))
  OR (OLD.change_type IN('platform_identity_suspend','platform_identity_reactivate','platform_identity_deactivate') AND EXISTS(
   SELECT 1 FROM system.principals WHERE id=OLD.target_principal_id AND status_change_request_id=OLD.id AND status=CASE OLD.change_type
    WHEN 'platform_identity_suspend' THEN 'suspended' WHEN 'platform_identity_reactivate' THEN 'active' ELSE 'deactivated' END))
 ) THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'invalid administrator request state transition' USING ERRCODE='23514';
END;
$$;
-- Retire approvals that predate the new immutable target binding.
UPDATE system.privileged_change_requests SET status='cancelled',decided_at=statement_timestamp(),updated_at=statement_timestamp()
WHERE status='approved';
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


CREATE OR REPLACE FUNCTION system.apply_principal_status_change_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>OLD.status AND NEW.status_change_request_id IS DISTINCT FROM OLD.status_change_request_id AND NEW.status_change_request_id IS NOT NULL THEN
  UPDATE system.privileged_change_requests SET status='applied',applied_at=statement_timestamp(),updated_at=statement_timestamp() WHERE id=NEW.status_change_request_id;
 END IF;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION system.apply_role_assignment_privileged_request() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE request_to_apply bigint;
BEGIN
 IF TG_OP='INSERT' THEN request_to_apply:=NEW.grant_change_request_id;
 ELSIF NEW.status='revoked' AND OLD.status='active' THEN request_to_apply:=NEW.revoke_change_request_id; END IF;
 IF request_to_apply IS NOT NULL THEN
  UPDATE system.privileged_change_requests req SET status='applied',applied_at=statement_timestamp(),updated_at=statement_timestamp()
  WHERE id=request_to_apply AND status='approved' AND (change_type<>'platform_role_replace' OR (
   EXISTS(SELECT 1 FROM system.role_assignments WHERE grant_change_request_id=req.id AND principal_id=req.successor_principal_id)
   AND EXISTS(SELECT 1 FROM system.role_assignments WHERE revoke_change_request_id=req.id AND id=req.target_assignment_id AND status='revoked')));
 END IF;
 RETURN NEW;
END;
$$;

UPDATE system.permissions SET status='disabled' WHERE permission_key LIKE 'iam.platform_identity_change.%';
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT 'iam.platform_administrator_change.'||action,'system',action,CASE WHEN action='read' THEN 'low' ELSE 'high' END,false,ARRAY['platform']::text[],false,
 'permissions.iam.platform_administrator_change.'||action||'.name','permissions.iam.platform_administrator_change.'||action||'.description','active'
FROM unnest(ARRAY['read','create','approve','reject']) action;
DELETE FROM system.role_permissions rp USING system.permissions p WHERE rp.permission_id=p.id AND p.permission_key LIKE 'iam.platform_identity_change.%';
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p ON p.permission_key LIKE 'iam.platform_administrator_change.%'
WHERE r.tenant_id IS NULL AND (
 (r.role_key='platform.system_administrator')
 OR (r.role_key='platform.security_administrator' AND p.action IN('read','create'))
 OR (r.role_key='platform.audit_administrator' AND p.action IN('read','approve','reject')));
UPDATE system.refresh_token_families f SET revoked_at=statement_timestamp(),revoked_reason='authorization_catalog_changed',updated_at=statement_timestamp()
WHERE f.revoked_at IS NULL AND EXISTS(SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE a.principal_id=f.principal_id
 AND a.status='active' AND a.scope_type='platform' AND r.role_key IN('platform.system_administrator','platform.security_administrator','platform.audit_administrator'));
COMMIT;
