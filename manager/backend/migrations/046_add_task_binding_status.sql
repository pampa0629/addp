ALTER TABLE manager.task_definitions
    ADD COLUMN IF NOT EXISTS binding_status VARCHAR(16) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS binding_issue VARCHAR(32) NOT NULL DEFAULT '';

UPDATE manager.task_definitions
SET binding_status = 'missing',
    binding_issue = last_execution_status,
    last_execution_status = NULL
WHERE last_execution_status IN ('missing_engine', 'missing_source');

CREATE INDEX IF NOT EXISTS idx_manager_task_definitions_tenant_binding_status
    ON manager.task_definitions (tenant_id, binding_status);
