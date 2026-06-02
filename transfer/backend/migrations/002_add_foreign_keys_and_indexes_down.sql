-- Migration Rollback: 002_add_foreign_keys_and_indexes
-- Description: 回滚外键约束和索引的添加
-- Date: 2026-01-14

BEGIN;

-- ============================================================
-- 1. 删除外键约束
-- ============================================================

ALTER TABLE transfer.tasks DROP CONSTRAINT IF EXISTS fk_tasks_source_engine;
ALTER TABLE transfer.tasks DROP CONSTRAINT IF EXISTS fk_tasks_target_engine;
ALTER TABLE transfer.task_executions DROP CONSTRAINT IF EXISTS fk_executions_task;

-- ============================================================
-- 2. 删除索引
-- ============================================================

-- 只删除新创建的索引
DROP INDEX IF EXISTS transfer.idx_tasks_tenant_type;
DROP INDEX IF EXISTS transfer.idx_tasks_tenant_creator_time;
DROP INDEX IF EXISTS transfer.idx_checkpoints_task_exec;

COMMIT;

-- Rollback completed
