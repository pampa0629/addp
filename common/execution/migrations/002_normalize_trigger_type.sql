-- 统一 common.task_executions.trigger_type 语义：
-- trigger_type 只表达触发方式，不表达来源模块、API 通道或重试场景。

UPDATE common.task_executions
SET trigger_type = 'scheduled'
WHERE trigger_type = 'schedule';

UPDATE common.task_executions
SET trigger_type = 'manual'
WHERE trigger_type IN ('api', 'orchestrator', 'retry', 'system_immediate');

ALTER TABLE common.task_executions
DROP CONSTRAINT IF EXISTS chk_task_executions_trigger_type;

ALTER TABLE common.task_executions
ADD CONSTRAINT chk_task_executions_trigger_type
CHECK (trigger_type IN ('manual', 'scheduled', 'event'));
