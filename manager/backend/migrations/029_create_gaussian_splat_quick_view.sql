CREATE TABLE IF NOT EXISTS manager.gaussian_splat_quick_view_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL,
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

CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tasks_tenant
    ON manager.gaussian_splat_quick_view_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tasks_last_execution
    ON manager.gaussian_splat_quick_view_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tasks_deleted_at
    ON manager.gaussian_splat_quick_view_tasks (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tasks_source_unique
    ON manager.gaussian_splat_quick_view_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
    WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> '';

CREATE TABLE IF NOT EXISTS manager.gaussian_splat_quick_view (
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

CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tenant_item
    ON manager.gaussian_splat_quick_view (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_tenant_item_fingerprint
    ON manager.gaussian_splat_quick_view (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_task
    ON manager.gaussian_splat_quick_view (task_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_execution
    ON manager.gaussian_splat_quick_view (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_status
    ON manager.gaussian_splat_quick_view (status);
CREATE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_deleted_at
    ON manager.gaussian_splat_quick_view (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gaussian_splat_quick_view_current_unique
    ON manager.gaussian_splat_quick_view (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';

COMMENT ON TABLE manager.gaussian_splat_quick_view_tasks IS '3DGS - KPlat 快显生成任务定义表';
COMMENT ON TABLE manager.gaussian_splat_quick_view IS 'Manager 受管 3DGS - KPlat 快显结果表';
