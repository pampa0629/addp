BEGIN;

-- Preserve all existing issuance and withdrawal history. A relation is derived,
-- not a second mutable ACL. Every row of one withdrawal carries its exact set.
ALTER TABLE system.engine_access_grant_revocations ADD COLUMN revoked_request_ids jsonb;
ALTER TABLE system.engine_access_grant_revocations DISABLE TRIGGER trg_engine_access_grant_revocation_immutable;
UPDATE system.engine_access_grant_revocations SET revoked_request_ids = jsonb_build_array(request_id);
ALTER TABLE system.engine_access_grant_revocations ENABLE TRIGGER trg_engine_access_grant_revocation_immutable;
ALTER TABLE system.engine_access_grant_revocations
    ALTER COLUMN revoked_request_ids SET NOT NULL,
    ADD CONSTRAINT engine_access_grant_revocation_set CHECK (
        jsonb_typeof(revoked_request_ids) = 'array' AND jsonb_array_length(revoked_request_ids) > 0
        AND revoked_request_ids @> jsonb_build_array(request_id));

-- Run AFTER the existing insert guard derives trusted Catalog receipt facts.
-- The service has already acquired its exact-target lock. The relation lock
-- also protects the invariant against simultaneous database insert statements.
CREATE FUNCTION system.guard_source_grant_relation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(
        'source_grant_relation|' || jsonb_build_array(NEW.tenant_id, NEW.engine_id,
            NEW.catalog_path, NEW.recipient_type, NEW.recipient_id, NEW.action)::text, 0));
    IF EXISTS (SELECT 1 FROM system.engine_access_grants g
        WHERE g.request_id <> NEW.request_id AND g.tenant_id = NEW.tenant_id AND g.engine_id = NEW.engine_id
            AND g.catalog_path = NEW.catalog_path AND g.recipient_type = NEW.recipient_type
            AND g.recipient_id = NEW.recipient_id AND g.action = NEW.action
            AND g.granted_at <= clock_timestamp()
            AND (g.expires_at IS NULL OR g.expires_at > clock_timestamp())
            AND NOT EXISTS (SELECT 1 FROM system.engine_access_grant_revocations r WHERE r.request_id = g.request_id)) THEN
        RAISE EXCEPTION 'source authorization relation already exists; explicit withdrawal is required before changing expiry'
            USING ERRCODE = '23505', CONSTRAINT = 'engine_access_grant_relation_exists';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_source_grant_relation AFTER INSERT ON system.engine_access_grants
    FOR EACH ROW EXECUTE FUNCTION system.guard_source_grant_relation();

COMMIT;
