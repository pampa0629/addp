BEGIN;
ALTER TABLE system.engines
    ADD COLUMN server_version text NOT NULL DEFAULT '',
    ADD COLUMN server_version_checked_at timestamptz;
COMMENT ON COLUMN system.engines.server_version IS 'Native product version from the last successful connection probe; not the aggregate revision';
COMMENT ON COLUMN system.engines.server_version_checked_at IS 'Time of the last successful native product version observation';
COMMIT;
