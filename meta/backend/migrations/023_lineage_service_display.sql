ALTER TABLE meta.lineage_service_dependencies
    ADD COLUMN IF NOT EXISTS service_name VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS service_updated_at TIMESTAMPTZ;

-- Names and current revisions are replayed by Service, their authoritative owner.
-- Unnamed pre-upgrade projections stay out of the current view until that replay.
UPDATE meta.lineage_service_dependencies
SET status = 'unverified'
WHERE status = 'active' AND service_name = '';
