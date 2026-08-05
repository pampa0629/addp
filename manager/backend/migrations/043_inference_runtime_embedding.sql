-- Clean break: Manager no longer owns Provider endpoints, upstream model names or API keys.
DROP TABLE IF EXISTS manager.embedding_configuration;
CREATE TABLE manager.embedding_configuration (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    version             BIGINT NOT NULL,
    max_distance        DOUBLE PRECISION NOT NULL,
    max_file_size_mb    INTEGER NOT NULL,
    batch_concurrency   INTEGER NOT NULL,
    updated_by          INTEGER NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS manager.inference_scenario_bindings (
    id                  SERIAL PRIMARY KEY,
    scenario_code       VARCHAR(80) NOT NULL,
    scope_type          VARCHAR(16) NOT NULL CHECK (scope_type IN ('platform', 'tenant')),
    tenant_id           INTEGER,
    model_profile_id    UUID NOT NULL,
    version             BIGINT NOT NULL,
    updated_by          INTEGER NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((scope_type = 'platform' AND tenant_id IS NULL) OR (scope_type = 'tenant' AND tenant_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_manager_inference_binding_platform
ON manager.inference_scenario_bindings (scenario_code)
WHERE scope_type = 'platform' AND tenant_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_manager_inference_binding_tenant
ON manager.inference_scenario_bindings (scenario_code, tenant_id)
WHERE scope_type = 'tenant' AND tenant_id IS NOT NULL;

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
    model_profile_id    UUID NOT NULL,
    profile_version     BIGINT NOT NULL,
    deployment_id       UUID NOT NULL,
    dimension           INTEGER NOT NULL,
    status              VARCHAR(32) NOT NULL CHECK (status IN ('ready', 'outdated', 'failed', 'unsupported', 'missing_source')),
    status_reason       VARCHAR(64),
    error_message       TEXT,
    last_execution_id   VARCHAR(36),
    vectorized_at       TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, item_fingerprint)
);
CREATE INDEX idx_embeddings_item_id ON manager.embeddings (item_id);
CREATE INDEX idx_embeddings_engine ON manager.embeddings (engine_id);
CREATE INDEX idx_embeddings_status ON manager.embeddings (status);
CREATE INDEX idx_embeddings_ready_query
ON manager.embeddings (tenant_id, status, model_profile_id, profile_version, dimension, engine_id);

-- Stored task snapshots from the removed model-name contract are not executable.
DELETE FROM manager.embedding_tasks;
