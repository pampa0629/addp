-- Transfer 阶段 1 只对外声明 task_type=import。
-- 内部导出、同步等语义后续专题确认，当前不保留多套任务类型并行。
-- 非 import 的旧任务定义不能被伪装成 import，否则 Orchestrator 会引用到语义错误的任务。

DELETE FROM transfer.transfer_tasks
WHERE task_type IS DISTINCT FROM 'import';

ALTER TABLE transfer.transfer_tasks
    ALTER COLUMN task_type SET DEFAULT 'import',
    ALTER COLUMN task_type SET NOT NULL;

-- 旧表名已由 008 迁移到 transfer.transfer_tasks；若历史库里仍被 AutoMigrate 或旧脚本留下空表，清理掉。
DROP TABLE IF EXISTS transfer.tasks;
