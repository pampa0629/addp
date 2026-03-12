-- 011_create_mvt_tasks.sql
-- 创建 MVT 瓦片生成任务定义表

CREATE TABLE IF NOT EXISTS manager.mvt_tasks (
    id                      SERIAL PRIMARY KEY,
    tenant_id               INTEGER NOT NULL,
    name                    VARCHAR(255) NOT NULL,
    description             TEXT,
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,

    -- 最近一次执行信息
    last_execution_id       VARCHAR(36),
    last_execution_status   VARCHAR(50),
    last_run_at             TIMESTAMPTZ,

    created_by              INTEGER,

    -- MVT 特有字段
    engine_id               INTEGER NOT NULL,
    schema_name             VARCHAR(255) NOT NULL,
    table_name              VARCHAR(255) NOT NULL,
    min_zoom                INTEGER NOT NULL DEFAULT 0,
    max_zoom                INTEGER NOT NULL DEFAULT 18,
    optimization_config     JSONB,

    -- 时间戳
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_mvt_tasks_tenant_id ON manager.mvt_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_mvt_tasks_engine_id ON manager.mvt_tasks (engine_id);
CREATE INDEX IF NOT EXISTS idx_mvt_tasks_deleted_at ON manager.mvt_tasks (deleted_at);
