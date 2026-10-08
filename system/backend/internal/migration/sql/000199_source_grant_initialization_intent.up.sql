BEGIN;
-- Immutable command intent distinguishes first configuration from an existing basis.
ALTER TABLE system.engine_access_grants ADD COLUMN initialized_approval boolean NOT NULL DEFAULT false;
ALTER TABLE system.engine_access_grants ADD CONSTRAINT engine_grant_initialization_intent
    CHECK (NOT initialized_approval OR (approval_mode = 'independent' AND requirement_version = 1));
COMMIT;
