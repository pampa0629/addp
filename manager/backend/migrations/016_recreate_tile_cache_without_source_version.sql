-- 016_recreate_tile_cache_without_source_version.sql
-- Clean break: tile cache no longer stores source/version signatures.

DROP TABLE IF EXISTS manager.tile_cache_artifacts;
DROP TABLE IF EXISTS manager.tile_cache;

CREATE TABLE manager.tile_cache (
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

CREATE INDEX idx_tile_cache_tenant_item_fingerprint ON manager.tile_cache (tenant_id, item_fingerprint);
CREATE INDEX idx_tile_cache_tenant_item ON manager.tile_cache (tenant_id, item_id);
CREATE INDEX idx_tile_cache_status ON manager.tile_cache (status);
CREATE INDEX idx_tile_cache_task ON manager.tile_cache (task_id);
CREATE INDEX idx_tile_cache_execution ON manager.tile_cache (last_execution_id);
CREATE INDEX idx_tile_cache_deleted_at ON manager.tile_cache (deleted_at);
CREATE UNIQUE INDEX idx_tile_cache_tenant_fingerprint_format_unique
    ON manager.tile_cache (tenant_id, item_fingerprint, tile_format)
    WHERE deleted_at IS NULL;
