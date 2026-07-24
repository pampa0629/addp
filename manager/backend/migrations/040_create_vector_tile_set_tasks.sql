CREATE TABLE IF NOT EXISTS manager.vector_tile_set_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL,
    schedule VARCHAR(255) NOT NULL DEFAULT '',
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_execution_id VARCHAR(36),
    last_execution_status VARCHAR(50),
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_vector_tile_set_tasks_tenant ON manager.vector_tile_set_tasks (tenant_id);
CREATE INDEX IF NOT EXISTS idx_vector_tile_set_tasks_last_execution ON manager.vector_tile_set_tasks (last_execution_id);
CREATE INDEX IF NOT EXISTS idx_vector_tile_set_tasks_deleted_at ON manager.vector_tile_set_tasks (deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vector_tile_set_tasks_semantic_unique
    ON manager.vector_tile_set_tasks (tenant_id, (config->>'semantic_hash'))
    WHERE deleted_at IS NULL;
