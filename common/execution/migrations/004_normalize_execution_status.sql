-- 统一 common.task_executions.status 语义：
-- 成功状态使用 success，不再使用 completed。

UPDATE common.task_executions
SET status = 'success'
WHERE status = 'completed';

ALTER TABLE common.task_executions
DROP CONSTRAINT IF EXISTS chk_task_executions_status;

ALTER TABLE common.task_executions
ADD CONSTRAINT chk_task_executions_status
CHECK (status IN ('pending', 'running', 'success', 'failed', 'timeout', 'cancelled'));
