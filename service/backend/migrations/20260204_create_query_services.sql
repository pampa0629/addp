-- ====================================================================
-- Service 模块重构 - Phase 1: 创建查询服务表
-- 日期: 2026-02-04
-- 目的: 实现查询服务的独立表结构
-- 变更:
--   1. 创建 query_services 表 (查询服务)
--   2. 支持两种配置方式: table (界面配置) 和 sql (SQL配置)
--   3. 使用 data_config JSONB 字段存储空间配置、默认字段、可过滤字段等
--   4. 支持 REST API 和 OGC Features 协议
-- ====================================================================

-- Step 1: 创建 query_services 表
CREATE TABLE IF NOT EXISTS service.query_services (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES system.tenants(id) ON DELETE CASCADE,
    service_name VARCHAR(255) NOT NULL,          -- 服务唯一标识（英文、数字、下划线）
    title VARCHAR(255) NOT NULL,                  -- 服务标题
    description TEXT,                             -- 服务描述
    keywords TEXT[],                              -- 关键词数组

    -- 配置方式（互斥）
    config_type VARCHAR(50) NOT NULL CHECK (config_type IN ('table', 'sql')),

    -- 存储引擎
    engine_id BIGINT NOT NULL REFERENCES system.engines(id) ON DELETE CASCADE,

    -- 表配置字段（config_type='table'时使用）
    schema_name VARCHAR(255),
    table_name VARCHAR(255),

    -- SQL配置字段（config_type='sql'时使用）
    sql_query TEXT,

    -- 数据配置（JSONB，包含空间字段、默认字段等配置）
    data_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    /* data_config 结构示例：
    {
      "geometry": {                               // 空间字段配置（如有）
        "has_geometry": true,
        "column": "geom",
        "srid": 4326,
        "types": ["Point"],
        "extent": {"minX": 116.0, "minY": 39.0, "maxX": 117.0, "maxY": 40.0}
      },
      "default_fields": ["id", "name", "geom"],  // 默认返回字段（Table模式）
      "filterable_fields": ["name", "category"]  // 可过滤字段（Table模式）
    }
    */

    -- 协议配置
    protocols JSONB NOT NULL DEFAULT '{
        "rest_api": {"enabled": true, "formats": ["json", "csv", "geojson"]},
        "ogc_features": {"enabled": false, "version": "1.0"}
    }'::jsonb,

    -- 访问控制
    public_access BOOLEAN DEFAULT FALSE,          -- 是否公开访问（无需认证）
    max_features INTEGER DEFAULT 1000,            -- 最大返回要素数

    -- 状态
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'error')),
    error_message TEXT,                           -- 错误信息

    -- 审计字段
    created_by BIGINT NOT NULL REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_query_service_name UNIQUE (tenant_id, service_name)
);

-- Step 2: 创建索引
CREATE INDEX IF NOT EXISTS idx_query_services_tenant ON service.query_services(tenant_id);
CREATE INDEX IF NOT EXISTS idx_query_services_config_type ON service.query_services(config_type);
CREATE INDEX IF NOT EXISTS idx_query_services_engine ON service.query_services(engine_id);
CREATE INDEX IF NOT EXISTS idx_query_services_status ON service.query_services(status);
CREATE INDEX IF NOT EXISTS idx_query_services_data_config_gin ON service.query_services USING GIN (data_config);
CREATE INDEX IF NOT EXISTS idx_query_services_protocols_gin ON service.query_services USING GIN (protocols);

-- Step 3: 添加注释
COMMENT ON TABLE service.query_services IS '查询服务表: 提供单数据源的REST API查询和OGC Features服务';
COMMENT ON COLUMN service.query_services.service_name IS '服务唯一标识，用于URL路径';
COMMENT ON COLUMN service.query_services.config_type IS '配置方式: table(界面配置) | sql(SQL配置)，创建后不可修改';
COMMENT ON COLUMN service.query_services.engine_id IS '关联的存储引擎ID';
COMMENT ON COLUMN service.query_services.schema_name IS '数据库Schema名称（仅table模式使用）';
COMMENT ON COLUMN service.query_services.table_name IS '数据表名称（仅table模式使用）';
COMMENT ON COLUMN service.query_services.sql_query IS 'SQL查询语句（仅sql模式使用）';
COMMENT ON COLUMN service.query_services.data_config IS '数据配置JSON: geometry(空间字段配置)、default_fields(默认返回字段)、filterable_fields(可过滤字段)';
COMMENT ON COLUMN service.query_services.protocols IS '协议配置JSON: rest_api、ogc_features';
COMMENT ON COLUMN service.query_services.public_access IS '是否允许公开访问（无需JWT认证）';
COMMENT ON COLUMN service.query_services.max_features IS '单次请求最大返回要素数';

-- Step 4: 添加触发器自动更新 updated_at
CREATE OR REPLACE FUNCTION service.update_query_services_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_query_services_updated_at
    BEFORE UPDATE ON service.query_services
    FOR EACH ROW
    EXECUTE FUNCTION service.update_query_services_updated_at();

-- Step 5: 验证变更
DO $$
DECLARE
    v_table_exists BOOLEAN;
    v_index_count INTEGER;
BEGIN
    -- 检查表是否存在
    SELECT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'service'
        AND table_name = 'query_services'
    ) INTO v_table_exists;

    IF NOT v_table_exists THEN
        RAISE EXCEPTION '迁移失败: query_services 表不存在';
    END IF;

    -- 检查索引数量
    SELECT COUNT(*) INTO v_index_count
    FROM pg_indexes
    WHERE schemaname = 'service'
    AND tablename = 'query_services';

    IF v_index_count < 6 THEN
        RAISE EXCEPTION '迁移失败: query_services 表索引数量不足';
    END IF;

    RAISE NOTICE '✓ 查询服务表创建成功';
    RAISE NOTICE '  - query_services 表已创建';
    RAISE NOTICE '  - % 个索引已创建', v_index_count;
    RAISE NOTICE '  - 触发器已创建';
    RAISE NOTICE '  - 注释已添加';
END $$;
