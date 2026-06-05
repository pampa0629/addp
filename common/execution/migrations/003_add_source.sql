-- execution source 只表达触发来源模块，不承载调度方式或具体业务场景。

ALTER TABLE common.task_executions
ADD COLUMN IF NOT EXISTS source VARCHAR(50) NOT NULL DEFAULT '';

UPDATE common.task_executions
SET source = module
WHERE source = '';

CREATE INDEX IF NOT EXISTS idx_task_executions_source
ON common.task_executions(source);
