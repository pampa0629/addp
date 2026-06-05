-- 废弃 meta.scan_task_runs，执行记录统一由 common.task_executions 管理
DROP TABLE IF EXISTS meta.scan_task_runs CASCADE;
