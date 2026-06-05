-- ============================================================================
-- ADDP 数据库清理脚本 - 删除 meta.meta_item.attributes 中的冗余字段
-- 用途: 清理 attributes JSONB 字段中的 relative_path 和 object_key
-- 日期: 2026-01-14
-- 重要: 执行前请务必备份数据库
-- ============================================================================

-- 备份命令（在执行此脚本前运行）：
-- pg_dump -h localhost -p 15432 -U addp -d addp -t meta.meta_item > /tmp/meta_item_backup_$(date +%Y%m%d_%H%M%S).sql

BEGIN;

-- 1. 统计需要清理的记录
SELECT
    '清理前统计' AS stage,
    COUNT(*) FILTER (WHERE attributes ? 'relative_path') AS has_relative_path,
    COUNT(*) FILTER (WHERE attributes ? 'object_key') AS has_object_key,
    COUNT(*) AS total
FROM meta.meta_item;

-- 2. 清理 relative_path 和 object_key
-- 使用 JSONB - 操作符删除指定的键
UPDATE meta.meta_item
SET attributes = attributes - 'relative_path' - 'object_key'
WHERE attributes ? 'relative_path' OR attributes ? 'object_key';

-- 3. 验证清理结果
SELECT
    '清理后统计' AS stage,
    COUNT(*) FILTER (WHERE attributes ? 'relative_path') AS has_relative_path,
    COUNT(*) FILTER (WHERE attributes ? 'object_key') AS has_object_key,
    COUNT(*) AS total
FROM meta.meta_item;

-- 4. 查看清理后的示例数据（对象存储类型）
SELECT
    id,
    name,
    fingerprint,
    attributes
FROM meta.meta_item
WHERE item_type = 'object'
LIMIT 5;

-- 5. 查看清理后的示例数据（数据库表类型）
SELECT
    id,
    name,
    fingerprint,
    attributes
FROM meta.meta_item
WHERE item_type = 'table'
LIMIT 5;

-- 确认无误后提交
COMMIT;

-- 如果有问题，执行 ROLLBACK; 回滚
-- ROLLBACK;
