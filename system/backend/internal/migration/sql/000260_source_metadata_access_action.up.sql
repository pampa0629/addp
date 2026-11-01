BEGIN;

-- One resource-rule model. No role/recipient assignment or new Grant is made.
ALTER TABLE system.engine_access_grants
    DROP CONSTRAINT engine_access_grant_action,
    ADD CONSTRAINT engine_access_grant_action CHECK (action IN ('read', 'metadata_read'));
ALTER TABLE system.engine_access_denies
    DROP CONSTRAINT engine_access_denies_action_check,
    ADD CONSTRAINT engine_access_denies_action_check CHECK (action IN ('read', 'metadata_read'));

COMMIT;
