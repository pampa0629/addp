BEGIN;

UPDATE system.module_definitions
SET enabled = true, version = version + 1, updated_at = now()
WHERE module_name = 'system' AND NOT enabled;

ALTER TABLE system.module_definitions
    ADD CONSTRAINT module_definitions_system_enabled
    CHECK (module_name <> 'system' OR enabled);

COMMIT;
