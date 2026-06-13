-- 012_alter_embedding_tasks_to_task_def.sql
-- 将 embedding_tasks 从执行记录型改为任务定义型
-- 旧的执行历史数据直接丢弃（当前处于积极开发阶段，无需兼容）

DROP TABLE IF EXISTS manager.embedding_tasks;

CREATE TABLE manager.embedding_tasks (
    id                      SERIAL PRIMARY KEY,
    tenant_id               INTEGER NOT NULL,
    name                    VARCHAR(255) NOT NULL,
    description             TEXT,
    enabled                 BOOLEAN NOT NULL,

    -- 最近一次执行信息（回写）
    last_execution_id       VARCHAR(36),
    last_execution_status   VARCHAR(50),
    last_run_at             TIMESTAMPTZ,
    next_run_at             TIMESTAMPTZ,
    schedule                VARCHAR(255),

    created_by              INTEGER,

    -- 向量化任务私有配置：config.target / config.filters / config.embedding
    config                  JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- 时间戳
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_embedding_tasks_tenant_id ON manager.embedding_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_embedding_tasks_next_run_at ON manager.embedding_tasks (next_run_at);
CREATE INDEX IF NOT EXISTS idx_embedding_tasks_deleted_at ON manager.embedding_tasks (deleted_at);
