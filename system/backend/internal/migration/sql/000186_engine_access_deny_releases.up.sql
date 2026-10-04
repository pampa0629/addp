BEGIN;

-- Releasing a Deny is independent from establishment and Grant withdrawal.
-- No built-in Role grants, Assignments or authorization-version updates.
INSERT INTO system.permissions (permission_key, owner_module, action, risk_level, delegable,
    allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key, status)
VALUES ('system.engine_access_deny.release', 'system', 'release', 'high', false,
    ARRAY['tenant']::text[], true, 'permissions.system.engine_access_deny.release.name',
    'permissions.system.engine_access_deny.release.description', 'active');

CREATE TABLE system.engine_access_deny_releases (
    deny_id uuid PRIMARY KEY REFERENCES system.engine_access_denies(deny_id),
    released_by_principal_id bigint NOT NULL REFERENCES system.principals(id),
    released_by_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
    released_at timestamptz NOT NULL CHECK (isfinite(released_at)),
    reason text NOT NULL CHECK (reason = btrim(reason) AND length(reason) BETWEEN 1 AND 2000)
);
CREATE INDEX idx_engine_access_deny_releases_principal ON system.engine_access_deny_releases(released_by_principal_id);
CREATE INDEX idx_engine_access_deny_releases_membership ON system.engine_access_deny_releases(released_by_membership_id);

CREATE FUNCTION system.guard_engine_access_deny_release_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    NEW.released_at := clock_timestamp();
    IF NOT EXISTS (
        SELECT 1 FROM system.engine_access_denies d
        JOIN system.tenant_memberships m ON m.tenant_id = d.tenant_id
        JOIN system.principals p ON p.id = m.principal_id
        WHERE d.deny_id = NEW.deny_id AND d.established_at <= NEW.released_at
          AND m.id = NEW.released_by_membership_id
          AND p.id = NEW.released_by_principal_id AND p.principal_type = 'user'
    ) THEN
        RAISE EXCEPTION 'deny releaser must reference a user membership in the original tenant'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_deny_releaser_binding';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM system.engine_access_denies d
        WHERE d.deny_id = NEW.deny_id AND (
            (d.expiry_mode = 'until_revoked' AND d.expires_at IS NULL)
            OR (d.expiry_mode = 'at_time' AND d.expires_at > NEW.released_at)
        )
    ) THEN
        RAISE EXCEPTION 'deny has expired; release is unnecessary'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_deny_release_expiry';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_engine_access_deny_release_insert BEFORE INSERT ON system.engine_access_deny_releases
FOR EACH ROW EXECUTE FUNCTION system.guard_engine_access_deny_release_insert();

CREATE FUNCTION system.preserve_engine_access_deny_release() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'engine access deny release history is immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER trg_engine_access_deny_release_immutable BEFORE UPDATE OR DELETE ON system.engine_access_deny_releases
FOR EACH ROW EXECUTE FUNCTION system.preserve_engine_access_deny_release();
CREATE TRIGGER trg_engine_access_deny_release_no_truncate BEFORE TRUNCATE ON system.engine_access_deny_releases
FOR EACH STATEMENT EXECUTE FUNCTION system.preserve_engine_access_deny_release();

COMMIT;
