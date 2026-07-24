DROP TABLE IF EXISTS transfer.schema_change_requests;
ALTER TABLE transfer.capture_resources DROP COLUMN IF EXISTS schema_revision;
