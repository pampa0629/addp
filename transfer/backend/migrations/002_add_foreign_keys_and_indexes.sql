-- Migration: 002_add_foreign_keys_and_indexes
-- Description: 添加外键约束和高性能索引，确保数据完整性和查询性能
-- Date: 2026-01-14
-- Related Issue: Transfer 模块优化方案 - 阶段 1.3

BEGIN;

-- ============================================================
-- 1. 添加外键约束
-- ============================================================

-- 1.1 tasks.source_id → system.engines.id
-- 当引擎删除时，任务保留但 source_id 置空
ALTER TABLE transfer.tasks
    ADD CONSTRAINT fk_tasks_source_engine
    FOREIGN KEY (source_id) REFERENCES system.engines(id)
    ON DELETE SET NULL;

-- 1.2 tasks.target_id → system.engines.id
-- 当引擎删除时，任务保留但 target_id 置空
ALTER TABLE transfer.tasks
    ADD CONSTRAINT fk_tasks_target_engine
    FOREIGN KEY (target_id) REFERENCES system.engines(id)
    ON DELETE SET NULL;

-- 1.3 task_executions.task_id → tasks.id
-- 当任务删除时，级联删除所有执行记录
ALTER TABLE transfer.task_executions
    ADD CONSTRAINT fk_executions_task
    FOREIGN KEY (task_id) REFERENCES transfer.tasks(id)
    ON DELETE CASCADE;

-- 1.4 data_mappings.task_id → tasks.id
-- 当任务删除时，级联删除所有字段映射
ALTER TABLE transfer.data_mappings
    ADD CONSTRAINT fk_mappings_task
    FOREIGN KEY (task_id) REFERENCES transfer.tasks(id)
    ON DELETE CASCADE;

-- ============================================================
-- 2. 添加高性能索引
-- ============================================================

-- 注意：以下索引已存在，跳过创建：
-- - idx_executions_task (task_executions.task_id)
-- - idx_executions_status (task_executions.status)
-- - idx_executions_start_time (task_executions.start_time)
-- - idx_transfer_tasks_tenant_id (tasks.tenant_id)
-- - idx_transfer_tasks_status (tasks.status)
-- - idx_transfer_data_mappings_task_id (data_mappings.task_id)

-- 2.1 tasks 表新增索引
-- 按租户和任务类型查询（用于筛选不同类型的任务）
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_type
    ON transfer.tasks(tenant_id, type);

-- 用户查询自己创建的任务（高频查询场景）
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_creator_time
    ON transfer.tasks(tenant_id, created_by, created_at DESC)
    WHERE created_by IS NOT NULL;

-- 2.4 checkpoints 表索引（如果表存在）
-- 按任务和执行记录查询 checkpoint
DO $$
BEGIN
    IF EXISTS (
        SELECT FROM information_schema.tables
        WHERE table_schema = 'transfer'
        AND table_name = 'checkpoints'
    ) THEN
        CREATE INDEX IF NOT EXISTS idx_checkpoints_task_exec
            ON transfer.checkpoints(task_id, execution_id, created_at DESC);
    END IF;
END $$;

-- ============================================================
-- 3. 验证迁移结果
-- ============================================================

-- 输出外键约束信息
DO $$
DECLARE
    fk_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO fk_count
    FROM information_schema.table_constraints
    WHERE constraint_schema = 'transfer'
    AND constraint_type = 'FOREIGN KEY';

    RAISE NOTICE '✅ Transfer schema 外键约束数量: %', fk_count;
END $$;

-- 输出索引信息
DO $$
DECLARE
    idx_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO idx_count
    FROM pg_indexes
    WHERE schemaname = 'transfer'
    AND indexname LIKE 'idx_%';

    RAISE NOTICE '✅ Transfer schema 自定义索引数量: %', idx_count;
END $$;

COMMIT;

-- Migration completed successfully
