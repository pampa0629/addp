-- =========================================================
-- System Engines 表重构迁移脚本
-- 日期: 2025-01-01
-- 目标:
--   1. 删除 name 字段（原英文标识）
--   2. display_name → name（显示名称）
--   3. 删除 unique_identifier 字段
--   4. 更新 engine_type 值（内置引擎简化）
--   5. 更新 capabilities 中的 dev_modes (sql → query)
-- =========================================================

-- 开始事务
BEGIN;

-- 步骤A：创建备份表（保留原始数据）
CREATE TABLE IF NOT EXISTS system.engines_backup_20250101 AS
SELECT * FROM system.engines;

RAISE NOTICE '✅ 备份表已创建: system.engines_backup_20250101';

-- 步骤B：重命名字段
ALTER TABLE system.engines RENAME COLUMN name TO _deprecated_identifier;
ALTER TABLE system.engines RENAME COLUMN display_name TO name;

RAISE NOTICE '✅ 字段重命名完成: display_name → name';

-- 步骤C：更新内置引擎的 engine_type（简化命名）
UPDATE system.engines
SET engine_type = 'python_workflow'
WHERE engine_type = 'api.python-workflow' AND is_builtin = true;

UPDATE system.engines
SET engine_type = 'spark_workflow'
WHERE engine_type = 'api.spark_workflow' AND is_builtin = true;

RAISE NOTICE '✅ 内置引擎 engine_type 已简化';

-- 步骤D：更新 capabilities 中的 dev_modes (sql → query)
UPDATE system.engines
SET capabilities = REPLACE(capabilities::text, '"dev_modes":["sql"]', '"dev_modes":["query"]')::jsonb
WHERE capabilities::text LIKE '%"dev_modes":["sql"]%';

RAISE NOTICE '✅ capabilities 中的 dev_modes 已更新: sql → query';

-- 步骤E：删除字段和索引
DROP INDEX IF EXISTS system.idx_unique_identifier;
DROP INDEX IF EXISTS system.idx_engines_name;

ALTER TABLE system.engines DROP COLUMN IF EXISTS unique_identifier;
ALTER TABLE system.engines DROP COLUMN _deprecated_identifier;

RAISE NOTICE '✅ 已删除字段: unique_identifier, _deprecated_identifier';

-- 步骤F：创建新索引
CREATE UNIQUE INDEX idx_builtin_engine_type
ON system.engines(engine_type)
WHERE is_builtin = true;

CREATE INDEX idx_engines_name ON system.engines(name);

RAISE NOTICE '✅ 新索引已创建';

-- 步骤G：验证迁移结果
DO $$
DECLARE
    col_count INT;
    builtin_count INT;
BEGIN
    -- 检查字段是否存在
    SELECT COUNT(*) INTO col_count
    FROM information_schema.columns
    WHERE table_schema='system'
      AND table_name='engines'
      AND column_name IN ('unique_identifier', '_deprecated_identifier', 'display_name');

    IF col_count > 0 THEN
        RAISE EXCEPTION '❌ 迁移失败：旧字段仍然存在';
    END IF;

    -- 检查 name 字段是否存在
    SELECT COUNT(*) INTO col_count
    FROM information_schema.columns
    WHERE table_schema='system'
      AND table_name='engines'
      AND column_name = 'name';

    IF col_count = 0 THEN
        RAISE EXCEPTION '❌ 迁移失败：name 字段不存在';
    END IF;

    -- 检查内置引擎
    SELECT COUNT(*) INTO builtin_count
    FROM system.engines
    WHERE is_builtin = true
      AND engine_type IN ('python_workflow', 'spark_workflow');

    IF builtin_count = 0 THEN
        RAISE WARNING '⚠️  未找到内置引擎，可能尚未注册';
    ELSE
        RAISE NOTICE '✅ 找到 % 个内置引擎', builtin_count;
    END IF;

    RAISE NOTICE '===========================================';
    RAISE NOTICE '✅ 数据库迁移成功完成';
    RAISE NOTICE '===========================================';
END $$;

-- 提交事务
COMMIT;

-- 显示迁移后的表结构
\d system.engines
