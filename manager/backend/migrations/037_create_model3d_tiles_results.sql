-- 037_create_model3d_tiles_results.sql
-- 增加 Manager infra 分块三维模型瓦片结果表。

CREATE TABLE IF NOT EXISTS manager.model3d_tiles (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    item_fingerprint VARCHAR(64) NOT NULL,
    item_id BIGINT,
    locator TEXT,
    task_id BIGINT,
    last_execution_id VARCHAR(36),
    source_engine_id BIGINT NOT NULL,
    source_format VARCHAR(64) NOT NULL,
    source_size_bytes BIGINT NOT NULL DEFAULT 0,
    target_format VARCHAR(32) NOT NULL,
    storage_ref TEXT NOT NULL,
    manifest_ref VARCHAR(512) NOT NULL,
    file_count BIGINT NOT NULL DEFAULT 0,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    error_message TEXT,
    created_by BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    CONSTRAINT chk_model3d_tiles_target_format CHECK (target_format IN ('3d_tiles', 's3m'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_model3d_tiles_current_unique
    ON manager.model3d_tiles (tenant_id, item_fingerprint, target_format)
    WHERE deleted_at IS NULL AND status <> 'deleted';
CREATE INDEX IF NOT EXISTS idx_model3d_tiles_status ON manager.model3d_tiles (status);
CREATE INDEX IF NOT EXISTS idx_model3d_tiles_task ON manager.model3d_tiles (task_id);
CREATE INDEX IF NOT EXISTS idx_model3d_tiles_execution ON manager.model3d_tiles (last_execution_id);

COMMENT ON TABLE manager.model3d_tiles IS 'Manager infra MinIO 分块三维模型瓦片结果表，target_format 区分 3D Tiles 与 S3M';
