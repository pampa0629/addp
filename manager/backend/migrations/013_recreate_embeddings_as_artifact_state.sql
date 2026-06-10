-- 013_recreate_embeddings_as_artifact_state.sql
-- Clean break: manager.embeddings is now the embedding artifact state table.
-- Historical fingerprint + modality / bucket + path + name storage is removed.

CREATE EXTENSION IF NOT EXISTS vector SCHEMA manager;

DROP TABLE IF EXISTS manager.document_embeddings;
DROP TABLE IF EXISTS manager.embeddings;

CREATE TABLE manager.embeddings (
    id                  SERIAL PRIMARY KEY,
    tenant_id           INTEGER NOT NULL,
    item_fingerprint    VARCHAR(64) NOT NULL,
    item_id             INTEGER NOT NULL,
    engine_id           INTEGER NOT NULL,
    locator             TEXT NOT NULL,
    source_version      VARCHAR(255) NOT NULL,
    embedding           manager.vector(2560),
    model               VARCHAR(100) NOT NULL,
    dimension           INTEGER NOT NULL,
    status              VARCHAR(32) NOT NULL,
    status_reason       VARCHAR(64),
    error_message       TEXT,
    last_execution_id   VARCHAR(36),
    vectorized_at       TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uk_embeddings_tenant_item_fingerprint UNIQUE (tenant_id, item_fingerprint),
    CONSTRAINT ck_embeddings_status CHECK (status IN ('ready', 'outdated', 'failed', 'unsupported', 'missing_source'))
);

CREATE INDEX IF NOT EXISTS idx_embeddings_item_id ON manager.embeddings (item_id);
CREATE INDEX IF NOT EXISTS idx_embeddings_engine ON manager.embeddings (engine_id);
CREATE INDEX IF NOT EXISTS idx_embeddings_status ON manager.embeddings (status);
CREATE INDEX IF NOT EXISTS idx_embeddings_ready_query
ON manager.embeddings (tenant_id, status, model, dimension, engine_id);
