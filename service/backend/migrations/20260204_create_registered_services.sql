-- ====================================================================
-- Service 模块重构 - Phase 2: 创建注册服务表
-- 日期: 2026-02-04
-- 目的: 实现注册外部第三方服务的表结构
-- 变更:
--   1. 创建 registered_services 表 (注册服务)
--   2. 创建 registered_service_layers 表 (注册服务图层)
--   3. 支持多种服务类型: WMS、WFS、WMTS、OGC API、XYZ、REST
--   4. 支持认证配置和健康检查
-- ====================================================================

-- Step 1: 创建 registered_services 表
CREATE TABLE IF NOT EXISTS service.registered_services (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES system.tenants(id) ON DELETE CASCADE,
    service_name VARCHAR(255) NOT NULL,          -- 服务唯一标识（英文、数字、下划线）
    title VARCHAR(255) NOT NULL,                  -- 服务标题
    description TEXT,                             -- 服务描述
    keywords TEXT[],                              -- 关键词数组

    -- 服务类型
    service_type VARCHAR(50) NOT NULL CHECK (service_type IN ('wms', 'wfs', 'wmts', 'ogc_api', 'xyz', 'rest')),

    -- 服务端点
    endpoint_url TEXT NOT NULL,                   -- 服务URL

    -- 元数据（GetCapabilities解析结果）
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 认证配置
    auth_type VARCHAR(50) DEFAULT 'none' CHECK (auth_type IN ('none', 'basic', 'bearer', 'api_key')),
    auth_config JSONB DEFAULT '{}'::jsonb,        -- 加密存储认证信息

    -- 健康检查
    health_check_url TEXT,                        -- 自定义健康检查端点
    last_checked_at TIMESTAMP WITH TIME ZONE,

    -- 状态
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'error')),
    error_message TEXT,

    -- 审计字段
    created_by BIGINT NOT NULL REFERENCES system.users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_registered_service_name UNIQUE (tenant_id, service_name)
);

-- Step 2: 创建 registered_service_layers 表
CREATE TABLE IF NOT EXISTS service.registered_service_layers (
    id BIGSERIAL PRIMARY KEY,
    service_id BIGINT NOT NULL REFERENCES service.registered_services(id) ON DELETE CASCADE,
    layer_name VARCHAR(255) NOT NULL,            -- 图层标识（从GetCapabilities解析）
    display_name VARCHAR(255),                    -- 图层显示名称
    description TEXT,

    -- 几何信息（从GetCapabilities解析）
    geometry_type VARCHAR(50),
    crs VARCHAR(50),                              -- 坐标参考系统
    bbox JSONB,                                   -- 边界框

    -- 图层元数据
    metadata JSONB DEFAULT '{}'::jsonb,

    -- 控制
    enabled BOOLEAN DEFAULT TRUE,

    -- 审计字段
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    CONSTRAINT unique_registered_layer_name UNIQUE (service_id, layer_name)
);

-- Step 3: 创建索引
CREATE INDEX IF NOT EXISTS idx_registered_services_tenant ON service.registered_services(tenant_id);
CREATE INDEX IF NOT EXISTS idx_registered_services_type ON service.registered_services(service_type);
CREATE INDEX IF NOT EXISTS idx_registered_services_status ON service.registered_services(status);
CREATE INDEX IF NOT EXISTS idx_registered_services_metadata_gin ON service.registered_services USING GIN (metadata);

CREATE INDEX IF NOT EXISTS idx_registered_layers_service ON service.registered_service_layers(service_id);
CREATE INDEX IF NOT EXISTS idx_registered_layers_enabled ON service.registered_service_layers(enabled);
CREATE INDEX IF NOT EXISTS idx_registered_layers_metadata_gin ON service.registered_service_layers USING GIN (metadata);

-- Step 4: 添加注释
COMMENT ON TABLE service.registered_services IS '注册服务表: 注册外部第三方服务（WMS、WFS、WMTS、OGC API、XYZ、REST）';
COMMENT ON COLUMN service.registered_services.service_name IS '服务唯一标识，用于URL路径';
COMMENT ON COLUMN service.registered_services.service_type IS '服务类型: wms | wfs | wmts | ogc_api | xyz | rest';
COMMENT ON COLUMN service.registered_services.endpoint_url IS '服务端点URL';
COMMENT ON COLUMN service.registered_services.metadata IS '服务元数据JSON: 从GetCapabilities解析的服务信息';
COMMENT ON COLUMN service.registered_services.auth_type IS '认证类型: none | basic | bearer | api_key';
COMMENT ON COLUMN service.registered_services.auth_config IS '认证配置JSON: 加密存储的认证信息';

COMMENT ON TABLE service.registered_service_layers IS '注册服务图层表: 存储外部服务的图层信息';
COMMENT ON COLUMN service.registered_service_layers.layer_name IS '图层标识（从GetCapabilities解析）';
COMMENT ON COLUMN service.registered_service_layers.geometry_type IS '几何类型（从GetCapabilities解析）';
COMMENT ON COLUMN service.registered_service_layers.crs IS '坐标参考系统（从GetCapabilities解析）';
COMMENT ON COLUMN service.registered_service_layers.bbox IS '边界框JSON（从GetCapabilities解析）';

-- Step 5: 添加触发器自动更新 updated_at
CREATE OR REPLACE FUNCTION service.update_registered_services_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_registered_services_updated_at
    BEFORE UPDATE ON service.registered_services
    FOR EACH ROW
    EXECUTE FUNCTION service.update_registered_services_updated_at();

CREATE OR REPLACE FUNCTION service.update_registered_service_layers_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_registered_service_layers_updated_at
    BEFORE UPDATE ON service.registered_service_layers
    FOR EACH ROW
    EXECUTE FUNCTION service.update_registered_service_layers_updated_at();

-- Step 6: 验证变更
DO $$
DECLARE
    v_services_exists BOOLEAN;
    v_layers_exists BOOLEAN;
    v_index_count INTEGER;
BEGIN
    -- 检查表是否存在
    SELECT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'service'
        AND table_name = 'registered_services'
    ) INTO v_services_exists;

    SELECT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'service'
        AND table_name = 'registered_service_layers'
    ) INTO v_layers_exists;

    IF NOT v_services_exists OR NOT v_layers_exists THEN
        RAISE EXCEPTION '迁移失败: registered_services 或 registered_service_layers 表不存在';
    END IF;

    -- 检查索引数量
    SELECT COUNT(*) INTO v_index_count
    FROM pg_indexes
    WHERE schemaname = 'service'
    AND (tablename = 'registered_services' OR tablename = 'registered_service_layers');

    IF v_index_count < 7 THEN
        RAISE EXCEPTION '迁移失败: 索引数量不足';
    END IF;

    RAISE NOTICE '✓ 注册服务表创建成功';
    RAISE NOTICE '  - registered_services 表已创建';
    RAISE NOTICE '  - registered_service_layers 表已创建';
    RAISE NOTICE '  - % 个索引已创建', v_index_count;
    RAISE NOTICE '  - 触发器已创建';
    RAISE NOTICE '  - 注释已添加';
END $$;
