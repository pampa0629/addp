-- 022_create_raster_cog.sql
-- Manager-owned COG quick view artifact state.

CREATE TABLE IF NOT EXISTS manager.raster_cog_tasks (
    id                      BIGSERIAL PRIMARY KEY,
    tenant_id               BIGINT NOT NULL,
    name                    VARCHAR(255) NOT NULL,
    description             TEXT,
    enabled                 BOOLEAN NOT NULL,
    schedule                VARCHAR(255),
    next_run_at             TIMESTAMPTZ,
    last_run_at             TIMESTAMPTZ,
    last_execution_id       VARCHAR(36),
    last_execution_status   VARCHAR(50),
    config                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by              BIGINT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS manager.raster_cog (
    id                      BIGSERIAL PRIMARY KEY,
    tenant_id               BIGINT NOT NULL,
    item_fingerprint        VARCHAR(64) NOT NULL,
    item_id                 BIGINT,
    locator                 TEXT,
    task_id                 BIGINT,
    last_execution_id       VARCHAR(36),
    source_engine_id        BIGINT NOT NULL,
    source_profile          VARCHAR(32),
    source_size_bytes       BIGINT,
    target_kind             VARCHAR(64) NOT NULL,
    storage_ref             TEXT NOT NULL,
    file_name               VARCHAR(512),
    size_bytes              BIGINT,
    width                   BIGINT,
    height                  BIGINT,
    band_count              BIGINT,
    source_srid             BIGINT,
    source_crs              TEXT,
    extent                  JSONB,
    extent_srid             BIGINT,
    status                  VARCHAR(32) NOT NULL,
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message           TEXT,
    created_by              BIGINT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_raster_cog_tasks_tenant
    ON manager.raster_cog_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_raster_cog_tasks_last_execution
    ON manager.raster_cog_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_raster_cog_tasks_deleted_at
    ON manager.raster_cog_tasks (deleted_at);
CREATE INDEX IF NOT EXISTS idx_raster_cog_tenant_item_fingerprint
    ON manager.raster_cog (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_raster_cog_tenant_item
    ON manager.raster_cog (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_raster_cog_task
    ON manager.raster_cog (task_id);
CREATE INDEX IF NOT EXISTS idx_raster_cog_execution
    ON manager.raster_cog (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_raster_cog_status
    ON manager.raster_cog (status);
CREATE INDEX IF NOT EXISTS idx_raster_cog_deleted_at
    ON manager.raster_cog (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_raster_cog_current_unique
    ON manager.raster_cog (tenant_id, item_fingerprint)
    WHERE deleted_at IS NULL AND status <> 'deleted';

COMMENT ON TABLE manager.raster_cog_tasks IS '栅格快显 COG 生成任务定义表';
COMMENT ON TABLE manager.raster_cog IS '栅格快显 COG 状态表';
