-- Service 模块：重命名 service_layers 表为 external_service_layers
-- 创建时间：2026-01-31
-- 说明：将外部服务图层表从 service_layers 重命名为 external_service_layers，使命名更清晰

-- 步骤 1：重命名表
ALTER TABLE IF EXISTS service.service_layers
RENAME TO external_service_layers;

-- 步骤 2：重命名索引（如果存在）
ALTER INDEX IF EXISTS service.idx_service_layer_service
RENAME TO idx_external_service_layer_service;

-- 步骤 3：重命名外键约束（如果存在）
DO $$
BEGIN
    -- 检查旧约束是否存在
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_schema = 'service'
        AND table_name = 'external_service_layers'
        AND constraint_name LIKE '%service_layers%'
    ) THEN
        -- 删除旧约束
        EXECUTE 'ALTER TABLE service.external_service_layers DROP CONSTRAINT IF EXISTS fk_service_layers_service';

        -- 创建新约束
        ALTER TABLE service.external_service_layers
        ADD CONSTRAINT fk_external_service_layers_service
        FOREIGN KEY (service_id)
        REFERENCES service.external_services(id)
        ON DELETE CASCADE;
    END IF;
END $$;

-- 验证迁移结果
SELECT
    'external_service_layers' as table_name,
    COUNT(*) as row_count,
    pg_size_pretty(pg_total_relation_size('service.external_service_layers')) as total_size
FROM service.external_service_layers;

-- 检查索引
SELECT
    indexname,
    indexdef
FROM pg_indexes
WHERE schemaname = 'service'
AND tablename = 'external_service_layers';

-- 检查外键约束
SELECT
    conname as constraint_name,
    contype as constraint_type,
    pg_get_constraintdef(oid) as definition
FROM pg_constraint
WHERE connamespace = 'service'::regnamespace
AND conrelid = 'service.external_service_layers'::regclass;
