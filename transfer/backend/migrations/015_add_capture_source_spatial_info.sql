ALTER TABLE transfer.capture_resources
    ADD COLUMN source_spatial_info JSONB NOT NULL DEFAULT '{}'::jsonb;
