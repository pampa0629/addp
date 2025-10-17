-- 为meta_item表添加fingerprint字段，用于数据去重和血缘追踪
-- 执行: psql -h localhost -U addp -d addp -f scripts/migrations/001_add_fingerprint_to_meta_item.sql

BEGIN;

-- 1. 添加fingerprint字段（SHA256哈希，64字符）
ALTER TABLE metadata.meta_item
ADD COLUMN IF NOT EXISTS fingerprint VARCHAR(64);

-- 2. 为现有记录生成fingerprint
-- 对象存储: SHA256(res_id || bucket || path)
-- 关系数据库: SHA256(res_id || schema || table_name)
UPDATE metadata.meta_item
SET fingerprint = ENCODE(
    SHA256(
        (res_id::TEXT ||
         COALESCE(attributes->>'bucket', '') ||
         COALESCE(attributes->>'path', '') ||
         COALESCE(attributes->>'relative_path', '') ||
         COALESCE(full_name, name)
        )::BYTEA
    ),
    'hex'
)
WHERE fingerprint IS NULL;

-- 3. 设置为NOT NULL（确保所有新记录都有fingerprint）
ALTER TABLE metadata.meta_item
ALTER COLUMN fingerprint SET NOT NULL;

-- 4. 创建唯一索引（防止重复）
CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_item_fingerprint
ON metadata.meta_item(fingerprint);

-- 5. 添加注释
COMMENT ON COLUMN metadata.meta_item.fingerprint IS '数据指纹：基于res_id+路径的SHA256哈希，用于去重和数据血缘追踪';

COMMIT;

-- 验证：查询重复的fingerprint
SELECT fingerprint, COUNT(*) as count
FROM metadata.meta_item
GROUP BY fingerprint
HAVING COUNT(*) > 1
ORDER BY count DESC;

-- 显示统计信息
SELECT
    COUNT(*) as total_items,
    COUNT(DISTINCT fingerprint) as unique_fingerprints,
    COUNT(*) - COUNT(DISTINCT fingerprint) as duplicates
FROM metadata.meta_item;
