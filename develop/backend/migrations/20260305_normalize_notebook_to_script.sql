-- Notebook 是 script 开发任务的当前实现形态，不再作为独立 task_type/dev_type。

UPDATE develop.dev_tasks
SET dev_type = 'script'
WHERE dev_type = 'notebook';

UPDATE common.task_executions
SET task_type = 'script'
WHERE module = 'develop'
  AND task_type = 'notebook';
