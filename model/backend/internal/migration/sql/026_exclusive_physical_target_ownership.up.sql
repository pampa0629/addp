-- A target is identified by the Engine Catalog path, not by a mutable Meta node ID.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model.logical_tables
        WHERE COALESCE(materialization->>'target_parent_locator', '') <> ''
          AND (substring(materialization->>'target_parent_locator' FROM '[?&]type=([^&]+)')
              IS NULL OR substring(materialization->>'target_parent_locator' FROM '[?&]type=([^&]+)')
              NOT IN ('schema', 'database'))
    ) THEN
        RAISE EXCEPTION 'model logical table has an invalid physical target locator';
    END IF;
END $$;

UPDATE model.logical_tables
SET materialization = jsonb_set(
    materialization,
    '{target_parent_locator}',
    to_jsonb(
        split_part(materialization->>'target_parent_locator', '?', 1)
        || '?type='
        || substring(materialization->>'target_parent_locator' FROM '[?&]type=([^&]+)')
    )
)
WHERE COALESCE(materialization->>'target_parent_locator', '') <> '';

-- The Model database owns the exclusive claim. Do not scope this index by Tenant:
-- one Engine Instance path denotes one physical table even across Tenants.
CREATE UNIQUE INDEX uq_model_logical_tables_physical_target
ON model.logical_tables (
    (materialization->>'target_parent_locator'),
    (materialization->>'target_name')
)
WHERE COALESCE(materialization->>'target_parent_locator', '') <> ''
  AND COALESCE(materialization->>'target_name', '') <> '';

-- This is a stable, non-reusable physical ownership proof. The external table
-- marker is only corroborating evidence; the target claim remains in Model.
ALTER TABLE model.logical_tables
    ADD COLUMN physical_owner_token UUID NOT NULL DEFAULT gen_random_uuid();
