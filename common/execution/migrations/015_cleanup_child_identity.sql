-- One Owner child per persistent cleanup request. Existing duplicates must be
-- investigated; migration never deletes, merges or invents historical facts.
CREATE UNIQUE INDEX IF NOT EXISTS uq_cleanup_parent_owner
ON common.task_executions (parent_execution_id, module)
WHERE task_type = 'cleanup_executor' AND parent_execution_id IS NOT NULL;
