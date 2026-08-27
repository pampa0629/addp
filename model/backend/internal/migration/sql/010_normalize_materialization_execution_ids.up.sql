ALTER TABLE model.materialization_batches
    ALTER COLUMN prepare_execution_id TYPE VARCHAR(255) USING prepare_execution_id::text,
    ALTER COLUMN publish_execution_id TYPE VARCHAR(255) USING publish_execution_id::text;

ALTER TABLE model.materialization_write_attempts
    ALTER COLUMN writer_execution_id TYPE VARCHAR(255) USING writer_execution_id::text,
    ALTER COLUMN model_execution_id TYPE VARCHAR(255) USING model_execution_id::text;
