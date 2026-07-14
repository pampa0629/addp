-- 027_create_model3d_tiles_tasks.sql
-- 分块三维模型瓦片任务定义表。

CREATE SCHEMA IF NOT EXISTS manager;

CREATE TABLE IF NOT EXISTS manager.model3d_tiles_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL,
    schedule VARCHAR(255),
    next_run_at TIMESTAMP,
    last_run_at TIMESTAMP,
    last_execution_id VARCHAR(36),
    last_execution_status VARCHAR(50),
    config JSONB NOT NULL DEFAULT '{}',
    created_by BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_model3d_tiles_tasks_tenant ON manager.model3d_tiles_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_model3d_tiles_tasks_last_execution ON manager.model3d_tiles_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_model3d_tiles_tasks_deleted_at ON manager.model3d_tiles_tasks (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_model3d_tiles_tasks_source_format_unique
    ON manager.model3d_tiles_tasks (
        tenant_id,
        ((config->'source'->>'item_fingerprint')),
        ((config->>'target_format'))
    )
    WHERE deleted_at IS NULL
      AND COALESCE(config->'source'->>'item_fingerprint', '') <> ''
      AND COALESCE(config->>'target_format', '') <> '';

COMMENT ON TABLE manager.model3d_tiles_tasks IS '分块三维模型瓦片任务定义表，target_format 区分 3D Tiles 与 S3M';
