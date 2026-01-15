-- ============================================================================
-- ADDP 路径字段统一迁移脚本
-- 用途: 将 manager.embeddings 表的 object_key 字段拆分为 path + name
-- 日期: 2026-01-13
-- ============================================================================

-- 开始事务
BEGIN;

-- ============================================================================
-- 1. 备份现有数据 (可选，建议在执行前先备份数据库)
-- ============================================================================
-- CREATE TABLE manager.embeddings_backup AS SELECT * FROM manager.embeddings;

-- ============================================================================
-- 2. 添加新字段
-- ============================================================================
ALTER TABLE manager.embeddings ADD COLUMN IF NOT EXISTS path TEXT;
ALTER TABLE manager.embeddings ADD COLUMN IF NOT EXISTS name VARCHAR(255);

-- ============================================================================
-- 3. 数据迁移: 拆分 object_key 为 path + name
-- ============================================================================
-- 规则:
-- - path: 目录路径（以/结尾），如 "image/" 或 "docs/reports/"
-- - name: 文件名，如 "开会.jpg"
-- - 示例: object_key="image/开会.jpg" -> path="image/", name="开会.jpg"
-- - 根目录文件: object_key="file.txt" -> path="", name="file.txt"

UPDATE manager.embeddings
SET
    path = CASE
        WHEN POSITION('/' IN object_key) > 0 THEN
            -- 提取最后一个 / 之前的部分（包含 /）
            SUBSTRING(object_key FROM 1 FOR LENGTH(object_key) - LENGTH(SPLIT_PART(object_key, '/', -1)))
        ELSE
            -- 根目录文件，path 为空
            ''
    END,
    name = CASE
        WHEN POSITION('/' IN object_key) > 0 THEN
            -- 提取最后一个 / 之后的部分
            SUBSTRING(object_key FROM (LENGTH(object_key) - LENGTH(SPLIT_PART(object_key, '/', -1)) + 2))
        ELSE
            -- 根目录文件，直接使用 object_key
            object_key
    END
WHERE path IS NULL OR name IS NULL;

-- ============================================================================
-- 4. 验证数据迁移结果
-- ============================================================================
-- 检查是否有记录的 name 为空
DO $$
DECLARE
    empty_name_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO empty_name_count FROM manager.embeddings WHERE name IS NULL OR name = '';
    IF empty_name_count > 0 THEN
        RAISE EXCEPTION '发现 % 条记录的 name 字段为空，请检查数据', empty_name_count;
    END IF;
    RAISE NOTICE '数据验证通过: 所有记录都有有效的 name 字段';
END $$;

-- ============================================================================
-- 5. 添加 NOT NULL 约束
-- ============================================================================
ALTER TABLE manager.embeddings ALTER COLUMN path SET NOT NULL;
ALTER TABLE manager.embeddings ALTER COLUMN name SET NOT NULL;

-- ============================================================================
-- 6. 创建索引
-- ============================================================================
-- 为 (bucket, path) 创建联合索引，优化目录级查询
CREATE INDEX IF NOT EXISTS idx_embeddings_bucket_path ON manager.embeddings(bucket, path);

-- ============================================================================
-- 7. 删除旧字段 (谨慎操作，建议在确认无误后再执行)
-- ============================================================================
-- 注释掉，需要手动确认后再执行
-- ALTER TABLE manager.embeddings DROP COLUMN object_key;

-- 提交事务
COMMIT;

-- ============================================================================
-- 验证查询示例
-- ============================================================================
-- 查看迁移后的数据样例
-- SELECT id, engine_id, bucket, path, name, object_key, fingerprint
-- FROM manager.embeddings
-- LIMIT 10;

-- 检查 path 字段格式（应该都以 / 结尾或为空）
-- SELECT path, COUNT(*) as count
-- FROM manager.embeddings
-- WHERE path != '' AND path NOT LIKE '%/'
-- GROUP BY path;

-- ============================================================================
-- 回滚脚本 (如果需要回退)
-- ============================================================================
-- BEGIN;
-- ALTER TABLE manager.embeddings DROP COLUMN IF EXISTS path;
-- ALTER TABLE manager.embeddings DROP COLUMN IF EXISTS name;
-- COMMIT;
