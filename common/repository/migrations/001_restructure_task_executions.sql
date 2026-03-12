-- Phase 1: 重构 common.task_executions
-- 统一字段命名，移除独立字段（合并入 metadata JSONB），新增 parent_execution_id
-- 无需数据迁移，直接重命名/删除/新增

-- 1. 重命名 execution_type → task_type
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = 'common'
               AND table_name = 'task_executions'
               AND column_name = 'execution_type') THEN
        ALTER TABLE common.task_executions RENAME COLUMN execution_type TO task_type;
    END IF;
END $$;

-- 2. 新增 metadata JSONB 字段
ALTER TABLE common.task_executions ADD COLUMN IF NOT EXISTS metadata JSONB;

-- 3. 新增 parent_execution_id（用于 Orchestrator 子步骤追踪父编排）
ALTER TABLE common.task_executions ADD COLUMN IF NOT EXISTS parent_execution_id VARCHAR(36);

-- 4. 删除已废弃的独立字段（合并入 metadata JSONB，无需数据迁移）
ALTER TABLE common.task_executions DROP COLUMN IF EXISTS result;
ALTER TABLE common.task_executions DROP COLUMN IF EXISTS checkpoint_offset;
ALTER TABLE common.task_executions DROP COLUMN IF EXISTS checkpoint_state;
ALTER TABLE common.task_executions DROP COLUMN IF EXISTS step_results;

-- 5. 更新索引：旧的 execution_type 索引已随列改名自动更新，
--    但为明确起见，重建复合索引（如果列名已改则旧索引自动失效需重建）
DROP INDEX IF EXISTS common.idx_task_executions_module_type;
CREATE INDEX IF NOT EXISTS idx_task_executions_module_type ON common.task_executions(module, task_type);

-- 6. 新增 parent_execution_id 索引
CREATE INDEX IF NOT EXISTS idx_task_executions_parent ON common.task_executions(parent_execution_id)
    WHERE parent_execution_id IS NOT NULL;
