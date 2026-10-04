BEGIN;

-- Explicit Deny is not IAM-wide ACL or an automatic Role assignment.
INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('system.engine_access_deny.create', 'system', 'create', 'high', false,
    ARRAY['tenant']::text[], true, 'permissions.system.engine_access_deny.create.name',
    'permissions.system.engine_access_deny.create.description', 'active');

CREATE TABLE system.engine_access_denies (
    deny_id uuid PRIMARY KEY CHECK (deny_id <> '00000000-0000-0000-0000-000000000000'),
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    engine_id bigint NOT NULL,
    catalog_path jsonb NOT NULL CHECK (jsonb_typeof(catalog_path) = 'object' AND catalog_path ? 'engine_id'
        AND (catalog_path->>'engine_id')::bigint = engine_id),
    recipient_type text NOT NULL CHECK (recipient_type IN ('user', 'department', 'project_group')),
    recipient_id bigint NOT NULL CHECK (recipient_id > 0),
    action text NOT NULL CHECK (action = 'read'),
    expiry_mode text NOT NULL CHECK (expiry_mode IN ('at_time', 'until_revoked')),
    expires_at timestamptz,
    established_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
    established_by_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
    established_at timestamptz NOT NULL CHECK (isfinite(established_at)),
    reason text NOT NULL CHECK (reason = btrim(reason) AND length(reason) BETWEEN 1 AND 2000),
    FOREIGN KEY (tenant_id, engine_id) REFERENCES system.engines(tenant_id, id),
    CHECK ((expiry_mode = 'until_revoked' AND expires_at IS NULL)
        OR (expiry_mode = 'at_time' AND expires_at IS NOT NULL AND isfinite(expires_at) AND expires_at > established_at))
);
CREATE INDEX idx_engine_access_denies_target ON system.engine_access_denies(tenant_id, engine_id);
CREATE INDEX idx_engine_access_denies_principal ON system.engine_access_denies(established_by_principal_id);
CREATE INDEX idx_engine_access_denies_membership ON system.engine_access_denies(established_by_membership_id);

CREATE FUNCTION system.guard_engine_access_deny_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    NEW.established_at := clock_timestamp();
    IF NEW.expiry_mode = 'at_time' AND NEW.expires_at <= NEW.established_at THEN
        RAISE EXCEPTION 'deny expiry must be future' USING ERRCODE = '23514', CONSTRAINT = 'engine_access_deny_expiry';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id
        WHERE m.id=NEW.established_by_membership_id AND m.tenant_id=NEW.tenant_id
          AND p.id=NEW.established_by_principal_id AND p.principal_type='user') THEN
        RAISE EXCEPTION 'deny actor must be a user membership in the original tenant' USING ERRCODE = '23514';
    END IF;
    IF NOT ((NEW.recipient_type='user' AND EXISTS (
            SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id
            WHERE m.tenant_id=NEW.tenant_id AND p.id=NEW.recipient_id AND p.principal_type='user'
              AND p.status='active' AND m.status='active' AND (m.expires_at IS NULL OR m.expires_at > NEW.established_at)))
        OR (NEW.recipient_type='department' AND EXISTS (
            SELECT 1 FROM system.departments d WHERE d.tenant_id=NEW.tenant_id AND d.id=NEW.recipient_id AND d.status='active'))
        OR (NEW.recipient_type='project_group' AND EXISTS (
            SELECT 1 FROM system.project_groups g WHERE g.tenant_id=NEW.tenant_id AND g.id=NEW.recipient_id AND g.status='active'))) THEN
        RAISE EXCEPTION 'deny subject must be a current recipient in the original tenant' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_engine_access_deny_insert BEFORE INSERT ON system.engine_access_denies
FOR EACH ROW EXECUTE FUNCTION system.guard_engine_access_deny_insert();

CREATE FUNCTION system.preserve_engine_access_deny() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'engine access deny history is immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER trg_engine_access_deny_immutable BEFORE UPDATE OR DELETE ON system.engine_access_denies
FOR EACH ROW EXECUTE FUNCTION system.preserve_engine_access_deny();
CREATE TRIGGER trg_engine_access_deny_no_truncate BEFORE TRUNCATE ON system.engine_access_denies
FOR EACH STATEMENT EXECUTE FUNCTION system.preserve_engine_access_deny();

COMMIT;
