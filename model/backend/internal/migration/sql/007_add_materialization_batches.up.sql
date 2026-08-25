CREATE TABLE model.materialization_batches (
    id UUID PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    logical_table_id BIGINT NOT NULL REFERENCES model.logical_tables(id) ON DELETE RESTRICT,
    logical_table_version BIGINT NOT NULL CHECK (logical_table_version > 0),
    engine_id BIGINT NOT NULL CHECK (engine_id > 0),
    target_parent_locator TEXT NOT NULL,
    target_name VARCHAR(63) NOT NULL,
    staging_name VARCHAR(63) NOT NULL,
    schema_fingerprint VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL,
    prepare_execution_id UUID NOT NULL UNIQUE,
    publish_execution_id UUID UNIQUE,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_materialization_batch_status CHECK (
        status IN ('preparing', 'prepared', 'publishing', 'published', 'failed', 'aborted')
    ),
    CONSTRAINT uq_model_materialization_batch_staging UNIQUE (engine_id, target_parent_locator, staging_name)
);

CREATE INDEX idx_model_materialization_batches_tenant_table
    ON model.materialization_batches(tenant_id, logical_table_id, created_at DESC);

CREATE UNIQUE INDEX uq_model_materialization_active_target
    ON model.materialization_batches(tenant_id, logical_table_id)
    WHERE status IN ('preparing', 'prepared', 'publishing');
