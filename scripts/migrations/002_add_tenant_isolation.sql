-- 002_add_tenant_isolation.sql
-- 为租户隔离添加数据库索引，优化权限校验查询性能

-- ===================================
-- Meta Node 表索引
-- ===================================

-- 租户 + 资源复合索引（最常用的查询组合）
CREATE INDEX IF NOT EXISTS idx_meta_node_tenant_resource
ON metadata.meta_node(tenant_id, res_id);

-- 租户 + 资源 + 父节点复合索引（用于获取子节点）
CREATE INDEX IF NOT EXISTS idx_meta_node_tenant_resource_parent
ON metadata.meta_node(tenant_id, res_id, parent_node_id)
WHERE deleted_at IS NULL;

-- 节点路径索引（用于路径查询）
CREATE INDEX IF NOT EXISTS idx_meta_node_tenant_path
ON metadata.meta_node(tenant_id, res_id, path)
WHERE deleted_at IS NULL;

-- ===================================
-- Meta Item 表索引
-- ===================================

-- 租户 + 节点复合索引
CREATE INDEX IF NOT EXISTS idx_meta_item_tenant_node
ON metadata.meta_item(tenant_id, node_id)
WHERE deleted_at IS NULL;

-- 租户 + 资源复合索引（用于直接按资源查询条目）
CREATE INDEX IF NOT EXISTS idx_meta_item_tenant_resource
ON metadata.meta_item(tenant_id, res_id)
WHERE deleted_at IS NULL;

-- 租户 + 资源 + 对象路径复合索引（用于对象存储元数据查询）
CREATE INDEX IF NOT EXISTS idx_meta_item_tenant_resource_path
ON metadata.meta_item(tenant_id, res_id, ((attributes->>'bucket')), ((attributes->>'path')))
WHERE deleted_at IS NULL AND item_type = 'object';

-- ===================================
-- 数据完整性检查和修复
-- ===================================

-- 检查是否有 tenant_id 不一致的数据
DO $$
DECLARE
    inconsistent_nodes INT;
    inconsistent_items INT;
BEGIN
    -- 检查 meta_node 中 tenant_id 与 resource.tenant_id 不一致的记录
    SELECT COUNT(*) INTO inconsistent_nodes
    FROM metadata.meta_node n
    JOIN system.resources r ON n.res_id = r.id
    WHERE n.tenant_id != r.tenant_id AND n.deleted_at IS NULL;

    IF inconsistent_nodes > 0 THEN
        RAISE NOTICE '发现 % 条 meta_node 记录的 tenant_id 与资源不一致，正在修复...', inconsistent_nodes;

        -- 修复 meta_node
        UPDATE metadata.meta_node n
        SET tenant_id = r.tenant_id
        FROM system.resources r
        WHERE n.res_id = r.id AND n.tenant_id != r.tenant_id AND n.deleted_at IS NULL;

        RAISE NOTICE 'meta_node 租户数据修复完成';
    ELSE
        RAISE NOTICE 'meta_node 租户数据一致性检查通过';
    END IF;

    -- 检查 meta_item 中 tenant_id 与 resource.tenant_id 不一致的记录
    SELECT COUNT(*) INTO inconsistent_items
    FROM metadata.meta_item i
    JOIN system.resources r ON i.res_id = r.id
    WHERE i.tenant_id != r.tenant_id AND i.deleted_at IS NULL;

    IF inconsistent_items > 0 THEN
        RAISE NOTICE '发现 % 条 meta_item 记录的 tenant_id 与资源不一致，正在修复...', inconsistent_items;

        -- 修复 meta_item
        UPDATE metadata.meta_item i
        SET tenant_id = r.tenant_id
        FROM system.resources r
        WHERE i.res_id = r.id AND i.tenant_id != r.tenant_id AND i.deleted_at IS NULL;

        RAISE NOTICE 'meta_item 租户数据修复完成';
    ELSE
        RAISE NOTICE 'meta_item 租户数据一致性检查通过';
    END IF;
END $$;

-- ===================================
-- 性能统计
-- ===================================

-- 输出索引创建结果
DO $$
DECLARE
    node_indexes INT;
    item_indexes INT;
BEGIN
    SELECT COUNT(*) INTO node_indexes
    FROM pg_indexes
    WHERE tablename = 'meta_node'
    AND schemaname = 'metadata'
    AND indexname LIKE 'idx_meta_node_tenant%';

    SELECT COUNT(*) INTO item_indexes
    FROM pg_indexes
    WHERE tablename = 'meta_item'
    AND schemaname = 'metadata'
    AND indexname LIKE 'idx_meta_item_tenant%';

    RAISE NOTICE '====================================';
    RAISE NOTICE '租户隔离索引创建完成';
    RAISE NOTICE 'meta_node 租户相关索引: %', node_indexes;
    RAISE NOTICE 'meta_item 租户相关索引: %', item_indexes;
    RAISE NOTICE '====================================';
END $$;

-- ===================================
-- 使用说明
-- ===================================

-- 1. 所有按资源查询的操作现在都会使用 (tenant_id, res_id) 索引
-- 2. 超级管理员 (tenant_id = 0) 的查询不会使用这些索引（但超级管理员操作较少）
-- 3. 定期运行 VACUUM ANALYZE 以更新统计信息：
--    VACUUM ANALYZE metadata.meta_node;
--    VACUUM ANALYZE metadata.meta_item;

-- 查看索引使用情况（生产环境运行一段时间后执行）：
-- SELECT
--     schemaname,
--     tablename,
--     indexname,
--     idx_scan as index_scans,
--     idx_tup_read as tuples_read,
--     idx_tup_fetch as tuples_fetched
-- FROM pg_stat_user_indexes
-- WHERE schemaname = 'metadata'
-- AND indexname LIKE 'idx_meta_%tenant%'
-- ORDER BY idx_scan DESC;
