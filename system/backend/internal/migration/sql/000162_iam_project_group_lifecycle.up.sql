BEGIN;

UPDATE system.project_groups
SET status = 'active', updated_at = now()
WHERE status = 'planned';

ALTER TABLE system.project_groups
    DROP CONSTRAINT project_groups_status_check;
ALTER TABLE system.project_groups
    ADD CONSTRAINT project_groups_status_check CHECK (status IN ('active', 'closed'));
ALTER TABLE system.project_groups
    ALTER COLUMN status SET DEFAULT 'active',
    DROP COLUMN starts_at,
    DROP COLUMN ends_at;

COMMIT;
