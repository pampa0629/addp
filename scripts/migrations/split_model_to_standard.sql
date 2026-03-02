-- ==================== Model 模块拆分迁移脚本 ====================
-- 目的：将 model schema 中的数据标准相关表移动到新的 standard schema
-- 执行时机：在创建 Standard 模块后、启动服务前执行
-- 执行方式：psql -h localhost -p 15432 -U addp -d addp -f scripts/migrations/split_model_to_standard.sql

BEGIN;

-- 创建 standard schema
CREATE SCHEMA IF NOT EXISTS standard;
COMMENT ON SCHEMA standard IS 'Standard 模块：数据标准管理（业务域、术语、数据元、码值集）';

-- 移动 6 个表到 standard schema
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'model') THEN
        RAISE NOTICE '📦 开始移动表到 standard schema...';

        -- 移动表
        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'domains') THEN
            ALTER TABLE model.domains SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 domains';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'glossaries') THEN
            ALTER TABLE model.glossaries SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 glossaries';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'glossary_element_mappings') THEN
            ALTER TABLE model.glossary_element_mappings SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 glossary_element_mappings';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'elements') THEN
            ALTER TABLE model.elements SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 elements';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'code_sets') THEN
            ALTER TABLE model.code_sets SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 code_sets';
        END IF;

        IF EXISTS (SELECT 1 FROM information_schema.tables
                   WHERE table_schema = 'model' AND table_name = 'code_items') THEN
            ALTER TABLE model.code_items SET SCHEMA standard;
            RAISE NOTICE '✅ 已移动 code_items';
        END IF;

        -- 删除跨 schema 外键约束
        RAISE NOTICE '🔗 删除跨 schema 外键约束...';
        ALTER TABLE model.logical_fields DROP CONSTRAINT IF EXISTS fk_logical_fields_element;
        ALTER TABLE model.entity_attributes DROP CONSTRAINT IF EXISTS fk_entity_attributes_element;
        RAISE NOTICE '✅ 已删除外键约束';

        RAISE NOTICE '✅ 迁移完成！';
    ELSE
        RAISE NOTICE '⏩ model schema 不存在，跳过迁移（可能是全新安装）';
    END IF;
END $$;

COMMIT;

-- 验证迁移结果
SELECT '=== standard schema 表数量 ===' AS info;
SELECT COUNT(*) AS table_count FROM information_schema.tables WHERE table_schema = 'standard';

SELECT '=== model schema 表数量 ===' AS info;
SELECT COUNT(*) AS table_count FROM information_schema.tables WHERE table_schema = 'model';
