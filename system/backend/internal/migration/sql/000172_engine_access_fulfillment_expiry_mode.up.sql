BEGIN;

-- Lossless protocol conversion, never a new Grant or extended expiry.
LOCK TABLE system.engine_access_fulfillment_outcomes IN ACCESS EXCLUSIVE MODE;
DROP TRIGGER trg_engine_access_fulfillment_immutable ON system.engine_access_fulfillment_outcomes;
ALTER TABLE system.engine_access_fulfillment_outcomes ADD COLUMN expiry_mode text;
UPDATE system.engine_access_fulfillment_outcomes
SET expiry_mode = 'at_time', binding = binding || '{"expiry_mode":"at_time"}'::jsonb;
ALTER TABLE system.engine_access_fulfillment_outcomes ALTER COLUMN expiry_mode SET NOT NULL;
ALTER TABLE system.engine_access_fulfillment_outcomes ALTER COLUMN grant_expires_at DROP NOT NULL;
ALTER TABLE system.engine_access_fulfillment_outcomes
    DROP CONSTRAINT engine_access_fulfillment_outcomes_grant_expires_at_check,
    DROP CONSTRAINT engine_access_fulfillment_outcomes_check;
ALTER TABLE system.engine_access_fulfillment_outcomes
    ADD CONSTRAINT engine_access_fulfillment_expiry_shape CHECK ((
        (expiry_mode = 'at_time' AND grant_expires_at IS NOT NULL AND isfinite(grant_expires_at))
        OR (expiry_mode = 'until_revoked' AND grant_expires_at IS NULL)) IS TRUE),
    ADD CONSTRAINT engine_access_fulfillment_expiry_binding CHECK ((
        binding->>'expiry_mode' = expiry_mode AND binding ? 'expires_at'
        AND ((expiry_mode = 'at_time' AND jsonb_typeof(binding->'expires_at') = 'string'
            AND (binding->>'expires_at')::timestamptz = grant_expires_at)
        OR (expiry_mode = 'until_revoked' AND binding->'expires_at' = 'null'::jsonb))) IS TRUE),
    ADD CONSTRAINT engine_access_fulfillment_deadline CHECK ((
        (outcome = 'closed' AND deadline IS NULL)
        OR (outcome = 'accepted' AND deadline IS NOT NULL AND isfinite(deadline)
            AND deadline > recorded_at
            AND deadline = CASE WHEN expiry_mode = 'at_time'
                THEN LEAST(recorded_at + interval '5 minutes', grant_expires_at)
                ELSE recorded_at + interval '5 minutes' END)) IS TRUE);
CREATE TRIGGER trg_engine_access_fulfillment_immutable BEFORE UPDATE OR DELETE
ON system.engine_access_fulfillment_outcomes FOR EACH ROW
EXECUTE FUNCTION system.preserve_engine_access_fulfillment_outcome();

COMMIT;
