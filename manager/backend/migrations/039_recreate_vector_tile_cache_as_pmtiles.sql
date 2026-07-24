-- Clean break: vector tile cache is a single PMTiles v3 archive identified by source + profile.
DELETE FROM common.task_executions
WHERE module = 'manager'
  AND task_type = 'vector_tile_cache_generation';

DROP TABLE IF EXISTS manager.vector_tile_cache;
DROP TABLE IF EXISTS manager.vector_tile_cache_tasks;

CREATE TABLE manager.vector_tile_cache_tasks (
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

CREATE UNIQUE INDEX idx_vector_tile_cache_tasks_source_profile_unique
    ON manager.vector_tile_cache_tasks (
        tenant_id,
        (config->'target'->>'item_fingerprint'),
        (config->>'profile_hash')
    )
    WHERE deleted_at IS NULL;

CREATE TABLE manager.vector_tile_cache (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    item_fingerprint VARCHAR(64) NOT NULL,
    item_id INTEGER,
    locator TEXT,
    task_id BIGINT,
    last_execution_id VARCHAR(36),
    tile_format VARCHAR(32) NOT NULL,
    storage_ref TEXT,
    source_version VARCHAR(64) NOT NULL,
    profile_hash VARCHAR(64) NOT NULL,
    extent JSONB,
    extent_srid INTEGER,
    min_zoom INTEGER,
    max_zoom INTEGER,
    status VARCHAR(32) NOT NULL,
    error_message TEXT,
    created_by INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_vector_tile_cache_tenant_fingerprint_profile_unique
    ON manager.vector_tile_cache (tenant_id, item_fingerprint, profile_hash)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_vector_tile_cache_tenant_item_fingerprint
    ON manager.vector_tile_cache (tenant_id, item_fingerprint);
CREATE INDEX idx_vector_tile_cache_status ON manager.vector_tile_cache (status);
CREATE INDEX idx_vector_tile_cache_task ON manager.vector_tile_cache (task_id);
CREATE INDEX idx_vector_tile_cache_execution ON manager.vector_tile_cache (last_execution_id);
