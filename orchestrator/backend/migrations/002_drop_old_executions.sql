-- 废弃 orchestrator.executions，执行记录统一由 common.task_executions 管理
DROP TABLE IF EXISTS orchestrator.executions CASCADE;
