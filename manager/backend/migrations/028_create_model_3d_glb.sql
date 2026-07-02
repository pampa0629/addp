CREATE TABLE IF NOT EXISTS manager.model_3d_glb_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    schedule VARCHAR(255),
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_execution_id VARCHAR(36),
    last_execution_status VARCHAR(50),
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_tenant
    ON manager.model_3d_glb_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_last_execution
    ON manager.model_3d_glb_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_deleted_at
    ON manager.model_3d_glb_tasks (deleted_at);
WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY tenant_id, config->'source'->>'item_fingerprint'
            ORDER BY updated_at DESC, id DESC
        ) AS rn
    FROM manager.model_3d_glb_tasks
    WHERE deleted_at IS NULL
        AND COALESCE(config->'source'->>'item_fingerprint', '') <> ''
)
UPDATE manager.model_3d_glb_tasks AS tasks
SET deleted_at = NOW(), updated_at = NOW(), enabled = FALSE
FROM ranked
WHERE tasks.id = ranked.id AND ranked.rn > 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_3d_glb_tasks_source_unique
    ON manager.model_3d_glb_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
    WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> '';

CREATE TABLE IF NOT EXISTS manager.model_3d_glb (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    item_fingerprint VARCHAR(64) NOT NULL,
    item_id BIGINT,
    locator TEXT,
    task_id BIGINT,
    last_execution_id VARCHAR(36),
    source_engine_id BIGINT NOT NULL,
    source_format VARCHAR(64) NOT NULL,
    source_size_bytes BIGINT DEFAULT 0,
    storage_ref TEXT NOT NULL,
    file_name VARCHAR(512),
    size_bytes BIGINT DEFAULT 0,
    content_url TEXT,
    status VARCHAR(32) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tenant_item
    ON manager.model_3d_glb (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_tenant_item_fingerprint
    ON manager.model_3d_glb (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_task
    ON manager.model_3d_glb (task_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_execution
    ON manager.model_3d_glb (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_status
    ON manager.model_3d_glb (status);
CREATE INDEX IF NOT EXISTS idx_model_3d_glb_deleted_at
    ON manager.model_3d_glb (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_3d_glb_current_unique
    ON manager.model_3d_glb (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';
