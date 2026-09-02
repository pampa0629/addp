-- `event` 已是 TaskExecution 的规范触发类型。早期环境在 002 迁移记录
-- 建立后仍可能保留只允许 manual/scheduled 的旧约束；必须通过新的前向
-- 迁移修正，不能修改已登记迁移或要求手工改库。

ALTER TABLE common.task_executions
DROP CONSTRAINT IF EXISTS chk_task_executions_trigger_type;

ALTER TABLE common.task_executions
ADD CONSTRAINT chk_task_executions_trigger_type
CHECK (trigger_type IN ('manual', 'scheduled', 'event'));
