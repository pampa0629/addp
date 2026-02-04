-- ====================================================================
-- Service 模块数据模型重构
-- 日期: 2026-02-02
-- 目的: 统一空间服务和数据表服务的发布模型
-- 变更:
--   1. 删除旧的协议标志位字段 (enabled_wfs, enabled_wmts, enabled_ogc_api, enabled_wms, enabled_rest_query)
--   2. 新增 service_type 字段 (spatial | table)
--   3. 新增 config JSONB 字段 (存储协议配置等)
--   4. 清空现有数据 (激进策略，不考虑兼容性)
-- ====================================================================

-- Step 1: 清空现有服务和图层数据
TRUNCATE TABLE service.internal_service_layers CASCADE;
TRUNCATE TABLE service.internal_services CASCADE;

-- Step 2: 重置自增 ID
ALTER SEQUENCE service.internal_services_id_seq RESTART WITH 1;
ALTER SEQUENCE service.internal_service_layers_id_seq RESTART WITH 1;

-- Step 3: 删除旧的协议字段
ALTER TABLE service.internal_services
DROP COLUMN IF EXISTS enabled_wfs,
DROP COLUMN IF EXISTS enabled_wmts,
DROP COLUMN IF EXISTS enabled_ogc_api,
DROP COLUMN IF EXISTS enabled_wms,
DROP COLUMN IF EXISTS enabled_rest_query;

-- Step 4: 添加新字段
ALTER TABLE service.internal_services
ADD COLUMN service_type VARCHAR(50) NOT NULL DEFAULT 'spatial',
ADD COLUMN config JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Step 5: 添加检查约束
ALTER TABLE service.internal_services
ADD CONSTRAINT check_service_type
CHECK (service_type IN ('spatial', 'table'));

-- Step 6: 添加索引
CREATE INDEX IF NOT EXISTS idx_internal_services_service_type ON service.internal_services(service_type);

-- Step 7: 添加注释
COMMENT ON COLUMN service.internal_services.service_type IS '服务类型: spatial(空间服务,支持多图层和OGC协议) | table(数据表服务,单图层,仅REST Query)';
COMMENT ON COLUMN service.internal_services.config IS '服务配置JSON: 协议配置(protocols)、多图层支持(allow_multiple_layers)等';

-- Step 8: 验证变更
DO $$
DECLARE
    v_service_type_exists BOOLEAN;
    v_config_exists BOOLEAN;
    v_enabled_wfs_exists BOOLEAN;
BEGIN
    -- 检查新字段是否存在
    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'service'
        AND table_name = 'internal_services'
        AND column_name = 'service_type'
    ) INTO v_service_type_exists;

    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'service'
        AND table_name = 'internal_services'
        AND column_name = 'config'
    ) INTO v_config_exists;

    -- 检查旧字段是否已删除
    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'service'
        AND table_name = 'internal_services'
        AND column_name = 'enabled_wfs'
    ) INTO v_enabled_wfs_exists;

    IF NOT v_service_type_exists THEN
        RAISE EXCEPTION '迁移失败: service_type 字段不存在';
    END IF;

    IF NOT v_config_exists THEN
        RAISE EXCEPTION '迁移失败: config 字段不存在';
    END IF;

    IF v_enabled_wfs_exists THEN
        RAISE EXCEPTION '迁移失败: enabled_wfs 字段未删除';
    END IF;

    RAISE NOTICE '✓ 数据库迁移成功完成';
    RAISE NOTICE '  - service_type 字段已添加';
    RAISE NOTICE '  - config 字段已添加';
    RAISE NOTICE '  - 旧的协议字段已删除';
    RAISE NOTICE '  - 现有数据已清空';
END $$;
