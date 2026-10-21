BEGIN;
CREATE TABLE system.emergency_session_requests (
 id uuid PRIMARY KEY,
 version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 tenant_id bigint NOT NULL REFERENCES system.tenants(id),
 executor_principal_id bigint NOT NULL REFERENCES system.principals(id),
 executor_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
 executor_authorization_version bigint NOT NULL CHECK(executor_authorization_version > 0),
 target_principal_id bigint NOT NULL REFERENCES system.principals(id),
 target_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
 target_authorization_version bigint NOT NULL CHECK(target_authorization_version > 0),
 target_family_id bigint NOT NULL REFERENCES system.refresh_token_families(id),
 reason text NOT NULL CHECK(btrim(reason) <> '' AND length(reason) <= 1000),
 incident_reference text NOT NULL CHECK(btrim(incident_reference) <> '' AND length(incident_reference) <= 200),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected','cancelled','revoked','executed')),
 requested_at timestamptz NOT NULL,
 approval_expires_at timestamptz NOT NULL,
 approved_at timestamptz,
 execute_before timestamptz,
 execution_id uuid UNIQUE,
 executed_at timestamptz,
 outcome text CHECK(outcome IN ('revoked','already_revoked','already_expired')),
 closed_by_principal_id bigint REFERENCES system.principals(id),
 close_reason text,
 CHECK(executor_principal_id <> target_principal_id),
 CHECK(approval_expires_at = requested_at + interval '10 minutes'),
 CHECK((approved_at IS NULL) = (execute_before IS NULL)),
 CHECK(approved_at IS NULL OR (approved_at >= requested_at AND approved_at < approval_expires_at AND execute_before = approved_at + interval '15 minutes')),
 CHECK(status='cancelled' OR ((status IN ('approved','revoked','executed')) = (approved_at IS NOT NULL))),
 CHECK((status='executed' AND execution_id IS NOT NULL AND executed_at IS NOT NULL AND outcome IS NOT NULL) OR (status<>'executed' AND execution_id IS NULL AND executed_at IS NULL AND outcome IS NULL)),
 CHECK(executed_at IS NULL OR (executed_at >= approved_at AND executed_at < execute_before)),
 CHECK((status IN ('rejected','cancelled','revoked') AND closed_by_principal_id IS NOT NULL AND close_reason IS NOT NULL) OR (status NOT IN ('rejected','cancelled','revoked') AND closed_by_principal_id IS NULL AND close_reason IS NULL)),
 CHECK(close_reason IS NULL OR btrim(close_reason) <> '')
);
CREATE INDEX idx_emergency_session_executor ON system.emergency_session_requests(tenant_id,executor_principal_id,requested_at DESC,id);
CREATE INDEX idx_emergency_session_pending ON system.emergency_session_requests(status,requested_at DESC,id);
CREATE TABLE system.emergency_session_approvals (
 request_id uuid NOT NULL REFERENCES system.emergency_session_requests(id),
 role_key text NOT NULL CHECK(role_key IN ('platform.system_administrator','platform.security_administrator')),
 principal_id bigint NOT NULL REFERENCES system.principals(id),
 authorization_version bigint NOT NULL CHECK(authorization_version > 0),
 reason text NOT NULL CHECK(btrim(reason) <> '' AND length(reason) <= 1000),
 approved_at timestamptz NOT NULL,
 PRIMARY KEY(request_id,role_key),
 UNIQUE(request_id,principal_id)
);
CREATE FUNCTION system.guard_emergency_session_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'emergency history is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP = 'INSERT' THEN
  IF NEW.status <> 'pending' OR NEW.version <> 1 THEN RAISE EXCEPTION 'new emergency request must be pending' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.tenant_id,NEW.executor_principal_id,NEW.executor_membership_id,NEW.executor_authorization_version,
        NEW.target_principal_id,NEW.target_membership_id,NEW.target_authorization_version,NEW.target_family_id,
        NEW.reason,NEW.incident_reference,NEW.requested_at,NEW.approval_expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.tenant_id,OLD.executor_principal_id,OLD.executor_membership_id,OLD.executor_authorization_version,
        OLD.target_principal_id,OLD.target_membership_id,OLD.target_authorization_version,OLD.target_family_id,
        OLD.reason,OLD.incident_reference,OLD.requested_at,OLD.approval_expires_at)
 OR NEW.version <> OLD.version + 1 OR OLD.status NOT IN ('pending','approved') THEN
  RAISE EXCEPTION 'immutable emergency binding or stale lifecycle' USING ERRCODE='23514';
 END IF;
 IF NOT (OLD.status='pending' AND NEW.status IN ('pending','approved','rejected','cancelled')
     OR OLD.status='approved' AND NEW.status IN ('revoked','cancelled','executed')) THEN
  RAISE EXCEPTION 'invalid emergency transition' USING ERRCODE='23514';
 END IF;
 IF OLD.approved_at IS NOT NULL AND ROW(NEW.approved_at,NEW.execute_before) IS DISTINCT FROM ROW(OLD.approved_at,OLD.execute_before) THEN
  RAISE EXCEPTION 'emergency deadline cannot be extended' USING ERRCODE='23514';
 END IF;
 IF NEW.status='approved' AND ((SELECT count(*) FROM system.emergency_session_approvals WHERE request_id=NEW.id) <> 2 OR NEW.approved_at IS DISTINCT FROM (SELECT max(approved_at) FROM system.emergency_session_approvals WHERE request_id=NEW.id)) THEN
  RAISE EXCEPTION 'two independent approvals required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_emergency_session_request_guard BEFORE INSERT OR UPDATE OR DELETE ON system.emergency_session_requests
 FOR EACH ROW EXECUTE FUNCTION system.guard_emergency_session_request();
CREATE FUNCTION system.guard_emergency_session_approval() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE req system.emergency_session_requests%ROWTYPE;
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'emergency approval is immutable' USING ERRCODE='23514'; END IF;
 SELECT * INTO req FROM system.emergency_session_requests WHERE id=NEW.request_id FOR UPDATE;
 IF req.status <> 'pending' OR NEW.principal_id IN (req.executor_principal_id,req.target_principal_id)
 OR NEW.approved_at < req.requested_at OR NEW.approved_at >= req.approval_expires_at THEN
  RAISE EXCEPTION 'invalid emergency approval binding' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER trg_emergency_session_approval_guard BEFORE INSERT OR UPDATE OR DELETE ON system.emergency_session_approvals
 FOR EACH ROW EXECUTE FUNCTION system.guard_emergency_session_approval();

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
SELECT 'iam.emergency_session_revocation.'||action,'system',action,CASE WHEN action='read' THEN 'low' ELSE 'high' END,false,
 CASE WHEN action='read' THEN ARRAY['platform','tenant']::text[] WHEN action IN ('approve','reject','revoke') THEN ARRAY['platform']::text[] ELSE ARRAY['tenant']::text[] END,
 action IN ('read','create','cancel','execute'),
 'permissions.iam.emergency_session_revocation.'||action||'.name','permissions.iam.emergency_session_revocation.'||action||'.description','active'
FROM unnest(ARRAY['approve','cancel','create','execute','read','reject','revoke']) action;
INSERT INTO system.role_permissions(role_id,permission_id,source_type)
SELECT r.id,p.id,'product' FROM system.roles r JOIN system.permissions p
 ON (r.role_key IN ('platform.system_administrator','platform.security_administrator') AND p.permission_key IN ('iam.emergency_session_revocation.approve','iam.emergency_session_revocation.read','iam.emergency_session_revocation.reject','iam.emergency_session_revocation.revoke'))
 OR (r.role_key='platform.audit_administrator' AND p.permission_key='iam.emergency_session_revocation.read')
WHERE r.tenant_id IS NULL AND r.role_type='platform_builtin';
-- Catalog triggers advance authorization versions; existing families must refresh by reauthentication.
UPDATE system.refresh_token_families f SET revoked_at=now(),revoked_reason='authorization_catalog_changed',updated_at=now()
WHERE f.revoked_at IS NULL AND EXISTS (SELECT 1 FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id
 WHERE a.principal_id=f.principal_id AND a.status='active' AND r.tenant_id IS NULL
 AND r.role_key IN ('platform.system_administrator','platform.security_administrator','platform.audit_administrator'));

COMMIT;
