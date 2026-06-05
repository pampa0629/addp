-- ScanTask 定义态收敛：范围进入 scope，任务归属使用 owner_module / owner_ref。
ALTER TABLE meta.scan_tasks
    ADD COLUMN IF NOT EXISTS scope JSONB,
    ADD COLUMN IF NOT EXISTS owner_module VARCHAR(50) NOT NULL DEFAULT 'meta',
    ADD COLUMN IF NOT EXISTS owner_ref VARCHAR(128);

UPDATE meta.scan_tasks
SET scope = CASE
    WHEN parameters ? 'catalog_paths'
         AND jsonb_typeof(parameters->'catalog_paths') = 'array'
         AND jsonb_array_length(parameters->'catalog_paths') > 0
    THEN jsonb_build_object(
        'type', 'catalog_path',
        'engine_id', engine_id,
        'catalog_paths', parameters->'catalog_paths'
    )
    ELSE jsonb_build_object(
        'type', 'engine',
        'engine_id', engine_id
    )
END
WHERE scope IS NULL OR scope = '{}'::jsonb;

UPDATE meta.scan_tasks
SET parameters = COALESCE(parameters, '{}'::jsonb) - 'catalog_paths';

ALTER TABLE meta.scan_tasks
    ALTER COLUMN scope SET DEFAULT '{}'::jsonb,
    ALTER COLUMN scope SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scan_tasks_owner
    ON meta.scan_tasks(owner_module, owner_ref);
