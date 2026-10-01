BEGIN;

-- Management qualification only. No data grant, role binding or source mutation.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
)
SELECT 'system.engine_access_delegation.' || action, 'system', action, risk, false,
       ARRAY['tenant']::text[], true,
       'permissions.system.engine_access_delegation.' || action || '.name',
       'permissions.system.engine_access_delegation.' || action || '.description', 'active'
FROM (VALUES ('create', 'high'), ('read', 'low'), ('revoke', 'high')) AS actions(action, risk);

CREATE TABLE system.engine_access_delegations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
    engine_id bigint NOT NULL,
    tenant_membership_id bigint NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    granted_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
    granted_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK (isfinite(granted_at)),
    expires_at timestamptz NOT NULL CHECK (isfinite(expires_at) AND expires_at > granted_at),
    grant_reason text NOT NULL CHECK (btrim(grant_reason) <> '' AND char_length(grant_reason) <= 2000),
    revoked_by_principal_id bigint REFERENCES system.principals(id),
    revoked_at timestamptz,
    revoked_reason text,
    FOREIGN KEY (tenant_id, engine_id) REFERENCES system.engines(tenant_id, id),
    FOREIGN KEY (tenant_id, tenant_membership_id) REFERENCES system.tenant_memberships(tenant_id, id),
    CHECK (
        (status = 'active' AND revoked_by_principal_id IS NULL AND revoked_at IS NULL AND revoked_reason IS NULL)
        OR (status = 'revoked' AND revoked_by_principal_id IS NOT NULL AND revoked_at IS NOT NULL
            AND revoked_reason IS NOT NULL AND btrim(revoked_reason) <> '' AND char_length(revoked_reason) <= 2000
            AND revoked_at >= granted_at AND revoked_at < expires_at)
    )
);
CREATE INDEX idx_engine_access_delegations_engine ON system.engine_access_delegations (tenant_id, engine_id, id DESC);
CREATE INDEX idx_engine_access_delegations_membership ON system.engine_access_delegations (tenant_id, tenant_membership_id);
CREATE INDEX idx_engine_access_delegations_grantor ON system.engine_access_delegations (granted_by_principal_id);
CREATE INDEX idx_engine_access_delegations_revoker ON system.engine_access_delegations (revoked_by_principal_id) WHERE revoked_by_principal_id IS NOT NULL;

CREATE FUNCTION system.guard_engine_access_delegation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'engine access delegation history cannot be deleted' USING ERRCODE = '23514';
    ELSIF TG_OP = 'INSERT' THEN
        IF NEW.expires_at <= clock_timestamp() THEN
            RAISE EXCEPTION 'engine access delegation expiry must be future' USING ERRCODE = '23514', CONSTRAINT = 'engine_access_delegations_expiry';
        END IF;
        IF NEW.status <> 'active' OR NEW.version <> 1 OR NEW.granted_at > clock_timestamp() THEN
            RAISE EXCEPTION 'invalid initial engine access delegation' USING ERRCODE = '23514';
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id = m.principal_id
            JOIN system.tenants t ON t.id = m.tenant_id
            WHERE m.id = NEW.tenant_membership_id AND m.tenant_id = NEW.tenant_id
              AND m.status = 'active' AND p.principal_type = 'user' AND p.status = 'active'
              AND t.status = 'active' AND (m.expires_at IS NULL OR m.expires_at >= NEW.expires_at)
        ) THEN
            RAISE EXCEPTION 'delegation requires an effective tenant user' USING ERRCODE = '23514';
        END IF;
        PERFORM pg_advisory_xact_lock(hashtextextended(concat_ws('|', 'engine_access_delegation', NEW.tenant_id, NEW.engine_id, NEW.tenant_membership_id), 0));
        IF EXISTS (
            SELECT 1 FROM system.engine_access_delegations d
            WHERE d.tenant_id = NEW.tenant_id AND d.engine_id = NEW.engine_id
              AND d.tenant_membership_id = NEW.tenant_membership_id AND d.status = 'active'
              AND tstzrange(d.granted_at, d.expires_at, '[)') && tstzrange(NEW.granted_at, NEW.expires_at, '[)')
        ) THEN
            RAISE EXCEPTION 'overlapping engine access delegation' USING ERRCODE = '23505', CONSTRAINT = 'engine_access_delegations_overlap';
        END IF;
    ELSE
        IF OLD.expires_at <= clock_timestamp() THEN
            RAISE EXCEPTION 'expired engine access delegation is read only' USING ERRCODE = '23514', CONSTRAINT = 'engine_access_delegations_expired_history';
        END IF;
        IF OLD.status <> 'active' OR NEW.status <> 'revoked'
           OR NEW.version <> OLD.version + 1
           OR (to_jsonb(NEW) - ARRAY['status','version','revoked_by_principal_id','revoked_at','revoked_reason'])
              IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['status','version','revoked_by_principal_id','revoked_at','revoked_reason']) THEN
            RAISE EXCEPTION 'engine access delegation is immutable except versioned revocation' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_engine_access_delegation_guard BEFORE INSERT OR UPDATE OR DELETE ON system.engine_access_delegations
FOR EACH ROW EXECUTE FUNCTION system.guard_engine_access_delegation();

COMMIT;
