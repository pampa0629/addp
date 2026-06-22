-- Transfer 阶段 1 只对外声明 task_type=sync。
-- Manager 导入 / 导出只是调用方业务语义，Transfer 任务类型不保留 import/export/transfer 双轨。
-- 非 sync 的旧任务定义不能被伪装成 sync，否则 Orchestrator 会引用到语义错误的任务。

DELETE FROM transfer.transfer_tasks
WHERE task_type IS DISTINCT FROM 'sync';

ALTER TABLE transfer.transfer_tasks
    ALTER COLUMN task_type SET DEFAULT 'sync',
    ALTER COLUMN task_type SET NOT NULL;

-- 旧表名已由 008 迁移到 transfer.transfer_tasks；若历史库里仍被 AutoMigrate 或旧脚本留下空表，清理掉。
DROP TABLE IF EXISTS transfer.tasks;
