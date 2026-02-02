-- Service 模块：回滚 external_service_layers 表重命名
-- 创建时间：2026-01-31
-- 说明：将 external_service_layers 表回滚为 service_layers（如需撤销迁移）

-- 步骤 1：重命名表
ALTER TABLE IF EXISTS service.external_service_layers
RENAME TO service_layers;

-- 步骤 2：重命名索引（如果存在）
ALTER INDEX IF EXISTS service.idx_external_service_layer_service
RENAME TO idx_service_layer_service;

-- 步骤 3：重命名外键约束（如果存在）
DO $$
BEGIN
    -- 检查新约束是否存在
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_schema = 'service'
        AND table_name = 'service_layers'
        AND constraint_name LIKE '%external_service_layers%'
    ) THEN
        -- 删除新约束
        EXECUTE 'ALTER TABLE service.service_layers DROP CONSTRAINT IF EXISTS fk_external_service_layers_service';

        -- 创建旧约束
        ALTER TABLE service.service_layers
        ADD CONSTRAINT fk_service_layers_service
        FOREIGN KEY (service_id)
        REFERENCES service.external_services(id)
        ON DELETE CASCADE;
    END IF;
END $$;

-- 验证回滚结果
SELECT
    'service_layers' as table_name,
    COUNT(*) as row_count,
    pg_size_pretty(pg_total_relation_size('service.service_layers')) as total_size
FROM service.service_layers;
