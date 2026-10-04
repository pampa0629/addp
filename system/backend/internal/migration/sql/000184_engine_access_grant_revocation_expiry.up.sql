BEGIN;

-- Tighten new insertions only. Never rewrite prior fulfillment, Grant or
-- revocation facts and never republish or assign permissions.
CREATE OR REPLACE FUNCTION system.guard_engine_access_grant_revocation_insert() RETURNS trigger
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
    IF NOT EXISTS (
        SELECT 1 FROM system.engine_access_fulfillment_outcomes o
        WHERE o.request_id = NEW.request_id AND (
            (o.expiry_mode = 'until_revoked' AND o.grant_expires_at IS NULL)
            OR (o.expiry_mode = 'at_time' AND o.grant_expires_at > NEW.revoked_at)
        )
    ) THEN
        RAISE EXCEPTION 'grant has expired; revocation is unnecessary'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_revocation_expiry';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
