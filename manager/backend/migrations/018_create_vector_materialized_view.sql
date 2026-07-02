-- 018_create_vector_materialized_view.sql
-- Manager vector materialized view task definitions and result state.

CREATE TABLE IF NOT EXISTS manager.vector_materialized_view_tasks (
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

CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_tenant
    ON manager.vector_materialized_view_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_schedule
    ON manager.vector_materialized_view_tasks (enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_last_execution
    ON manager.vector_materialized_view_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tasks_deleted_at
    ON manager.vector_materialized_view_tasks (deleted_at);

CREATE TABLE IF NOT EXISTS manager.vector_materialized_view (
    id                              BIGSERIAL PRIMARY KEY,
    tenant_id                       BIGINT NOT NULL,
    item_fingerprint                VARCHAR(64) NOT NULL,
    item_id                         BIGINT,
    locator                         TEXT,
    task_id                         BIGINT,
    last_execution_id               VARCHAR(36),
    source_engine_id                BIGINT NOT NULL,
    source_schema                   VARCHAR(255) NOT NULL,
    source_table                    VARCHAR(255) NOT NULL,
    source_geometry_column          VARCHAR(255) NOT NULL,
    source_srid                     BIGINT NOT NULL,
    target_srid                     BIGINT NOT NULL,
    target_kind                     VARCHAR(64) NOT NULL,
    target_schema                   VARCHAR(255) NOT NULL,
    target_table                    VARCHAR(255) NOT NULL,
    target_geometry_column          VARCHAR(255) NOT NULL,
    status                          VARCHAR(32) NOT NULL,
    render_extent                   JSONB,
    render_extent_srid              BIGINT,
    row_count_estimate              BIGINT,
    source_fingerprint_snapshot     JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata                        JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message                   TEXT,
    created_by                      BIGINT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at                      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tenant_item_fingerprint
    ON manager.vector_materialized_view (tenant_id, item_fingerprint);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_tenant_item
    ON manager.vector_materialized_view (tenant_id, item_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_task
    ON manager.vector_materialized_view (task_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_execution
    ON manager.vector_materialized_view (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_status
    ON manager.vector_materialized_view (status);
CREATE INDEX IF NOT EXISTS idx_vector_materialized_view_deleted_at
    ON manager.vector_materialized_view (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vector_materialized_view_current_unique
    ON manager.vector_materialized_view (tenant_id, item_fingerprint, source_geometry_column, target_srid)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE manager.vector_materialized_view_tasks IS '矢量物化视图任务定义表';
COMMENT ON TABLE manager.vector_materialized_view IS '矢量物化视图结果状态表';
