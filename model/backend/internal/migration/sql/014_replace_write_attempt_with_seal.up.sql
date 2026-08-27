UPDATE model.materialization_batches
SET status = 'aborted',
    updated_at = NOW()
WHERE status IN ('preparing', 'prepared', 'publishing');

DROP INDEX model.uq_model_materialization_active_target;

ALTER TABLE model.materialization_batches
    DROP CONSTRAINT IF EXISTS fk_model_materialization_completed_write_attempt,
    DROP CONSTRAINT ck_model_materialization_batch_status,
    DROP COLUMN completed_write_attempt_id,
    ALTER COLUMN staging_name DROP DEFAULT,
    ADD COLUMN writer_execution_id VARCHAR(255),
    ADD COLUMN seal_execution_id VARCHAR(255);

DROP TABLE model.materialization_write_attempts;

ALTER TABLE model.materialization_batches
    ADD CONSTRAINT ck_model_materialization_batch_status CHECK (
        status IN ('preparing', 'prepared', 'sealed', 'publishing', 'published', 'failed', 'aborted')
    );

CREATE UNIQUE INDEX uq_model_materialization_active_target
    ON model.materialization_batches(tenant_id, engine_id, target_parent_locator, target_name)
    WHERE status IN ('preparing', 'prepared', 'sealed', 'publishing');

CREATE UNIQUE INDEX uq_model_materialization_seal_execution
    ON model.materialization_batches(seal_execution_id)
    WHERE seal_execution_id IS NOT NULL;
