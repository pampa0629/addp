-- 026_create_raster_mosaic_tasks.sql
-- Raster mosaic generation task definitions. The generated mosaic dataset is a
-- business data item in the selected target storage, not a Manager artifact.

CREATE TABLE IF NOT EXISTS manager.raster_mosaic_tasks (
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

CREATE INDEX IF NOT EXISTS idx_raster_mosaic_tasks_tenant
    ON manager.raster_mosaic_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_raster_mosaic_tasks_last_execution
    ON manager.raster_mosaic_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_raster_mosaic_tasks_deleted_at
    ON manager.raster_mosaic_tasks (deleted_at);

COMMENT ON TABLE manager.raster_mosaic_tasks IS '栅格 mosaic 生成任务定义表';
