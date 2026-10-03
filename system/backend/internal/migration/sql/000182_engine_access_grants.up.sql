BEGIN;

-- One issuance per immutable accepted fulfillment. Target, recipient, action
-- and expiry remain in the original outcome; do not copy authorization facts.
CREATE TABLE system.engine_access_grants (
    request_id uuid PRIMARY KEY REFERENCES system.engine_access_fulfillment_outcomes(request_id),
    granted_at timestamptz NOT NULL CHECK (isfinite(granted_at))
);

CREATE FUNCTION system.guard_engine_access_grant_insert() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    accepted_at timestamptz;
    accepted_deadline timestamptz;
BEGIN
    SELECT recorded_at, deadline INTO accepted_at, accepted_deadline
    FROM system.engine_access_fulfillment_outcomes
    WHERE request_id = NEW.request_id AND outcome = 'accepted';
    -- Never accept a backdated application timestamp or transaction start time.
    NEW.granted_at := clock_timestamp();
    IF accepted_deadline IS NULL OR NEW.granted_at < accepted_at
        OR NEW.granted_at >= accepted_deadline THEN
        RAISE EXCEPTION 'engine access grant requires an unexpired accepted fulfillment'
            USING ERRCODE = '23514', CONSTRAINT = 'engine_access_grant_acceptance_window';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_engine_access_grant_insert BEFORE INSERT
ON system.engine_access_grants FOR EACH ROW
EXECUTE FUNCTION system.guard_engine_access_grant_insert();

-- This is issuance history, not current access validity. Future revocation
-- must not rewrite the original result returned for an idempotent retry.
CREATE FUNCTION system.preserve_engine_access_grant() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    RAISE EXCEPTION 'engine access grant issuance is immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER trg_engine_access_grant_immutable BEFORE UPDATE OR DELETE
ON system.engine_access_grants FOR EACH ROW
EXECUTE FUNCTION system.preserve_engine_access_grant();
CREATE TRIGGER trg_engine_access_grant_no_truncate BEFORE TRUNCATE
ON system.engine_access_grants FOR EACH STATEMENT
EXECUTE FUNCTION system.preserve_engine_access_grant();

COMMIT;
