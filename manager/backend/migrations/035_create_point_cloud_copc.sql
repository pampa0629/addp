CREATE TABLE IF NOT EXISTS manager.point_cloud_copc_tasks (
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

CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_tasks_tenant
    ON manager.point_cloud_copc_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_tasks_last_execution
    ON manager.point_cloud_copc_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_tasks_deleted_at
    ON manager.point_cloud_copc_tasks (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_point_cloud_copc_tasks_source_unique
    ON manager.point_cloud_copc_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
    WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> '';

CREATE TABLE IF NOT EXISTS manager.point_cloud_copc (
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

CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_tenant_item
    ON manager.point_cloud_copc (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_tenant_item_fingerprint
    ON manager.point_cloud_copc (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_task
    ON manager.point_cloud_copc (task_id);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_execution
    ON manager.point_cloud_copc (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_status
    ON manager.point_cloud_copc (status);
CREATE INDEX IF NOT EXISTS idx_point_cloud_copc_deleted_at
    ON manager.point_cloud_copc (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_point_cloud_copc_current_unique
    ON manager.point_cloud_copc (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';

COMMENT ON TABLE manager.point_cloud_copc_tasks IS '点云 COPC 快显生成任务定义表';
COMMENT ON TABLE manager.point_cloud_copc IS 'Manager 受管点云 COPC 快显结果表';
