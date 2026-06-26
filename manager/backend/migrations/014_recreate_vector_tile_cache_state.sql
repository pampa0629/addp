-- 014_recreate_vector_tile_cache_state.sql
-- Clean break: MVT task definitions and Quick View cache-state columns are replaced
-- by tile cache task definitions, tile cache result state, and compact quick view preference.

DO $$
BEGIN
    EXECUTE 'DROP TABLE IF EXISTS manager.' || quote_ident('mvt' || '_tasks');
END $$;
DROP TABLE IF EXISTS manager.quick_view;
DROP TABLE IF EXISTS manager.vector_tile_cache;
DROP TABLE IF EXISTS manager.vector_tile_cache_tasks;

CREATE TABLE manager.vector_tile_cache_tasks (
    id                      SERIAL PRIMARY KEY,
    tenant_id               INTEGER NOT NULL,
    name                    VARCHAR(255) NOT NULL,
    description             TEXT,
    enabled                 BOOLEAN NOT NULL,
    schedule                VARCHAR(255),
    next_run_at             TIMESTAMPTZ,
    last_run_at             TIMESTAMPTZ,
    last_execution_id       VARCHAR(36),
    last_execution_status   VARCHAR(50),
    config                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by              INTEGER,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE INDEX idx_vector_tile_cache_tasks_tenant ON manager.vector_tile_cache_tasks (tenant_id);
CREATE INDEX idx_vector_tile_cache_tasks_schedule ON manager.vector_tile_cache_tasks (enabled, next_run_at);
CREATE INDEX idx_vector_tile_cache_tasks_last_execution ON manager.vector_tile_cache_tasks (last_execution_id);
CREATE INDEX idx_vector_tile_cache_tasks_deleted_at ON manager.vector_tile_cache_tasks (deleted_at);

CREATE TABLE manager.vector_tile_cache (
    id                      SERIAL PRIMARY KEY,
    tenant_id               INTEGER NOT NULL,
    item_fingerprint        VARCHAR(64) NOT NULL,
    item_id                 INTEGER,
    locator                 TEXT,
    task_id                 INTEGER,
    last_execution_id       VARCHAR(36),
    tile_format             VARCHAR(32) NOT NULL,
    storage_ref             TEXT,
    extent                  JSONB,
    extent_srid             INTEGER,
    min_zoom                INTEGER,
    max_zoom                INTEGER,
    status                  VARCHAR(32) NOT NULL,
    error_message           TEXT,
    created_by              INTEGER,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE INDEX idx_vector_tile_cache_tenant_item_fingerprint ON manager.vector_tile_cache (tenant_id, item_fingerprint);
CREATE INDEX idx_vector_tile_cache_tenant_item ON manager.vector_tile_cache (tenant_id, item_id);
CREATE INDEX idx_vector_tile_cache_status ON manager.vector_tile_cache (status);
CREATE INDEX idx_vector_tile_cache_task ON manager.vector_tile_cache (task_id);
CREATE INDEX idx_vector_tile_cache_execution ON manager.vector_tile_cache (last_execution_id);
CREATE INDEX idx_vector_tile_cache_deleted_at ON manager.vector_tile_cache (deleted_at);
CREATE UNIQUE INDEX idx_vector_tile_cache_tenant_fingerprint_format_unique
    ON manager.vector_tile_cache (tenant_id, item_fingerprint, tile_format)
    WHERE deleted_at IS NULL;

CREATE TABLE manager.quick_view (
    id                          SERIAL PRIMARY KEY,
    tenant_id                   INTEGER NOT NULL,
    item_fingerprint            VARCHAR(64) NOT NULL,
    locator                     TEXT,
    preferred_mode              VARCHAR(32) NOT NULL DEFAULT 'basic_preview',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_quick_view_tenant_fingerprint
    ON manager.quick_view (tenant_id, item_fingerprint);

COMMENT ON TABLE manager.vector_tile_cache_tasks IS '瓦片缓存生成任务定义表';
COMMENT ON TABLE manager.vector_tile_cache IS '瓦片缓存结果状态表';
COMMENT ON TABLE manager.quick_view IS 'Manager 空间预览用户偏好表，快显能力查询时动态合成';
