ALTER TABLE model.materialization_batches
    DROP CONSTRAINT IF EXISTS materialization_batches_publish_execution_id_key;

CREATE INDEX IF NOT EXISTS idx_model_materialization_batches_publish_execution
    ON model.materialization_batches(tenant_id, publish_execution_id)
    WHERE publish_execution_id IS NOT NULL;
