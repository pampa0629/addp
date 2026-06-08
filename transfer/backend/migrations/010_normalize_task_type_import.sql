-- Transfer 阶段 1 只对外声明 task_type=import。
-- 内部导出、同步等语义后续专题确认，当前不保留多套任务类型并行。

UPDATE transfer.transfer_tasks
SET task_type = 'import'
WHERE task_type IS DISTINCT FROM 'import';

ALTER TABLE transfer.transfer_tasks
    ALTER COLUMN task_type SET DEFAULT 'import',
    ALTER COLUMN task_type SET NOT NULL;
