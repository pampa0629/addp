-- Step 5: orchestrator.orchestrations 补充 BaseTask 基类字段
ALTER TABLE orchestrator.orchestrations
    ADD COLUMN IF NOT EXISTS last_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_execution_id VARCHAR(36),
    ADD COLUMN IF NOT EXISTS last_execution_status VARCHAR(20),
    ADD COLUMN IF NOT EXISTS created_by INTEGER;
