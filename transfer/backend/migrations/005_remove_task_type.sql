-- 删除旧任务类型字段。Transfer 当前唯一任务类型字段是 task_type，固定为 sync。

ALTER TABLE transfer.transfer_tasks DROP COLUMN IF EXISTS type;
