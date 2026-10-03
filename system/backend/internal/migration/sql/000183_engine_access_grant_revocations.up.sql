BEGIN;

-- Publishing the command does not delegate it to any Role or user.
INSERT INTO system.permissions (
    permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status
) VALUES (
    'system.engine_access_grant.revoke', 'system', 'revoke', 'high', false,
    ARRAY['tenant']::text[], true,
    'permissions.system.engine_access_grant.revoke.name',
    'permissions.system.engine_access_grant.revoke.description', 'active'
);

-- One immutable revocation per Grant. Scope and recipient remain authoritative
-- in the original fulfillment; no copy of those authorization parameters.
CREATE TABLE system.engine_access_grant_revocations (
    request_id uuid PRIMARY KEY REFERENCES system.engine_access_grants(request_id),
    revoked_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
    revoked_by_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
    revoked_at timestamptz NOT NULL CHECK (isfinite(revoked_at)),
    reason text NOT NULL CHECK (reason = btrim(reason) AND length(reason) BETWEEN 1 AND 2000)
);
CREATE INDEX idx_engine_access_grant_revocations_principal
    ON system.engine_access_grant_revocations(revoked_by_principal_id);
CREATE INDEX idx_engine_access_grant_revocations_membership
    ON system.engine_access_grant_revocations(revoked_by_membership_id);

CREATE FUNCTION system.guard_engine_access_grant_revocation_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    NEW.revoked_at := clock_timestamp();
    IF NOT EXISTS (
        SELECT 1 FROM system.engine_access_grants g
        JOIN system.engine_access_fulfillment_outcomes o USING (request_id)
        JOIN system.tenant_memberships m ON m.tenant_id = o.tenant_id
        JOIN system.principals p ON p.id = m.principal_id
        WHERE g.request_id = NEW.request_id AND g.granted_at <= NEW.revoked_at
          AND m.id = NEW.revoked_by_membership_id
          AND p.id = NEW.revoked_by_principal_id AND p.principal_type = 'user'
    ) THEN
        RAISE EXCEPTION 'grant revoker must reference a user membership in the original tenant'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_revoker_binding';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_engine_access_grant_revocation_insert BEFORE INSERT
ON system.engine_access_grant_revocations FOR EACH ROW
EXECUTE FUNCTION system.guard_engine_access_grant_revocation_insert();

CREATE FUNCTION system.preserve_engine_access_grant_revocation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'engine access grant revocation is immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER trg_engine_access_grant_revocation_immutable BEFORE UPDATE OR DELETE
ON system.engine_access_grant_revocations FOR EACH ROW
EXECUTE FUNCTION system.preserve_engine_access_grant_revocation();
CREATE TRIGGER trg_engine_access_grant_revocation_no_truncate BEFORE TRUNCATE
ON system.engine_access_grant_revocations FOR EACH STATEMENT
EXECUTE FUNCTION system.preserve_engine_access_grant_revocation();

COMMIT;
