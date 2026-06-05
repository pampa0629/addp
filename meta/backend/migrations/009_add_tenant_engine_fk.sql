-- Migration: 添加租户-引擎外键约束
-- Purpose: 确保元数据节点的 tenant_id 必须与引擎的 tenant_id 一致
-- Date: 2026-01-20

-- 1. 先清理不一致的数据
-- 1.1 清理 meta_item（需要先删除，因为它引用 meta_node）
DELETE FROM meta.meta_item
WHERE (tenant_id, engine_id) NOT IN (
    SELECT tenant_id, id FROM system.engines
);

-- 1.2 清理 meta_node
DELETE FROM meta.meta_node
WHERE (tenant_id, engine_id) NOT IN (
    SELECT tenant_id, id FROM system.engines
);

-- 2. 在 system.engines 表上创建复合唯一索引（如果不存在）
-- 注意：(tenant_id, id) 组合本身应该是唯一的，因为 id 是主键
CREATE UNIQUE INDEX IF NOT EXISTS idx_engines_tenant_engine_unique
ON system.engines (tenant_id, id);

-- 3. 在 meta.meta_node 表上添加外键约束
-- 确保 (tenant_id, engine_id) 必须引用 system.engines (tenant_id, id)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_meta_node_tenant_engine'
          AND conrelid = 'meta.meta_node'::regclass
    ) THEN
        ALTER TABLE meta.meta_node
        ADD CONSTRAINT fk_meta_node_tenant_engine
        FOREIGN KEY (tenant_id, engine_id)
        REFERENCES system.engines (tenant_id, id)
        ON DELETE CASCADE;
    END IF;
END $$;

-- 4. 同样为 meta.meta_item 添加外键约束
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_meta_item_tenant_engine'
          AND conrelid = 'meta.meta_item'::regclass
    ) THEN
        ALTER TABLE meta.meta_item
        ADD CONSTRAINT fk_meta_item_tenant_engine
        FOREIGN KEY (tenant_id, engine_id)
        REFERENCES system.engines (tenant_id, id)
        ON DELETE CASCADE;
    END IF;
END $$;

-- 说明：
-- 1. 此约束确保元数据的 tenant_id 必须与引擎的 tenant_id 一致
-- 2. CASCADE 删除：当引擎被删除时，自动删除对应的元数据
-- 3. 这是数据库层面的最后一道防线，防止应用层逻辑错误
