CREATE TABLE IF NOT EXISTS manager.cad_preview_tasks (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_cad_preview_tasks_source_unique
    ON manager.cad_preview_tasks (tenant_id, ((config->'source'->>'item_fingerprint')))
    WHERE deleted_at IS NULL AND COALESCE(config->'source'->>'item_fingerprint', '') <> '';
CREATE INDEX IF NOT EXISTS idx_cad_preview_tasks_tenant ON manager.cad_preview_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_cad_preview_tasks_last_execution ON manager.cad_preview_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_cad_preview_tasks_deleted_at ON manager.cad_preview_tasks (deleted_at);

CREATE TABLE IF NOT EXISTS manager.cad_previews (
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
    manifest_ref VARCHAR(512) NOT NULL DEFAULT 'manifest.json',
    thumbnail_ref VARCHAR(512),
    tile_count BIGINT DEFAULT 0,
    tile_size INTEGER DEFAULT 0,
    min_zoom INTEGER DEFAULT 0,
    max_zoom INTEGER DEFAULT 0,
    bounds JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cad_previews_current_unique
    ON manager.cad_previews (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';
CREATE INDEX IF NOT EXISTS idx_cad_previews_tenant_fingerprint ON manager.cad_previews (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_cad_previews_status ON manager.cad_previews (status);
CREATE INDEX IF NOT EXISTS idx_cad_previews_deleted_at ON manager.cad_previews (deleted_at);

COMMENT ON TABLE manager.cad_preview_tasks IS 'CAD 栅格瓦片预览生成任务定义表';
COMMENT ON TABLE manager.cad_previews IS 'Manager 受管 CAD 栅格瓦片预览结果表';
