UPDATE model.materialization_batches
SET status = 'aborted',
    updated_at = NOW()
WHERE status IN ('preparing', 'prepared', 'publishing');

ALTER TABLE model.materialization_batches
    DROP CONSTRAINT uq_model_materialization_batch_staging;

ALTER TABLE model.materialization_batches
    ALTER COLUMN staging_name SET DEFAULT '',
    ADD COLUMN completed_write_attempt_id UUID;

CREATE UNIQUE INDEX uq_model_materialization_batch_staging
    ON model.materialization_batches(engine_id, target_parent_locator, staging_name)
    WHERE staging_name <> '';

CREATE TABLE model.materialization_write_attempts (
    id UUID PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    batch_id UUID NOT NULL REFERENCES model.materialization_batches(id) ON DELETE CASCADE,
    writer_execution_id UUID NOT NULL,
    writer_attempt INTEGER NOT NULL CHECK (writer_attempt > 0),
    writer_module VARCHAR(50) NOT NULL CHECK (writer_module IN ('transfer', 'develop')),
    model_execution_id UUID NOT NULL UNIQUE,
    staging_name VARCHAR(63) NOT NULL,
    status VARCHAR(20) NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_model_materialization_write_attempt_status CHECK (
        status IN ('creating', 'ready', 'completed', 'superseded', 'failed')
    ),
    CONSTRAINT uq_model_materialization_writer_attempt UNIQUE (writer_execution_id, writer_attempt),
    CONSTRAINT uq_model_materialization_attempt_staging UNIQUE (batch_id, staging_name)
);

CREATE INDEX idx_model_materialization_write_attempts_batch
    ON model.materialization_write_attempts(tenant_id, batch_id, created_at DESC);

CREATE UNIQUE INDEX uq_model_materialization_current_write_attempt
    ON model.materialization_write_attempts(batch_id)
    WHERE status IN ('creating', 'ready');

CREATE UNIQUE INDEX uq_model_materialization_completed_write_attempt
    ON model.materialization_write_attempts(batch_id)
    WHERE status = 'completed';

ALTER TABLE model.materialization_batches
    ADD CONSTRAINT fk_model_materialization_completed_write_attempt
    FOREIGN KEY (completed_write_attempt_id)
    REFERENCES model.materialization_write_attempts(id)
    ON DELETE RESTRICT;
