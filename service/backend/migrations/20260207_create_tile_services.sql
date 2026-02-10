-- ============================================================================
-- 瓦片服务模块数据库迁移脚本
-- 创建时间: 2026-02-07
-- 说明: 创建 tile_services 和 tile_service_layers 表
-- ============================================================================

-- 创建 service schema（如果不存在）
CREATE SCHEMA IF NOT EXISTS service;

-- ============================================================================
-- 瓦片服务主表
-- ============================================================================
CREATE TABLE IF NOT EXISTS service.tile_services (
    id SERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    service_name VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    keywords TEXT[],

    -- 瓦片配置
    default_srid INTEGER DEFAULT 3857,  -- Web Mercator
    extent JSONB,  -- 所有图层的联合边界，格式: [minX, minY, maxX, maxY]

    -- 协议配置
    protocols JSONB NOT NULL DEFAULT '{
        "xyz": {"enabled": true, "formats": ["mvt"]},
        "ogc_tiles": {"enabled": false},
        "tms": {"enabled": false}
    }'::jsonb,

    -- 访问控制
    public_access BOOLEAN DEFAULT FALSE,

    -- 状态
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'error')),
    error_message TEXT,

    -- 审计字段
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- 唯一约束：同一租户下服务名称唯一
    CONSTRAINT unique_tile_service_name UNIQUE (tenant_id, service_name)
);

-- ============================================================================
-- 瓦片服务图层表
-- ============================================================================
CREATE TABLE IF NOT EXISTS service.tile_service_layers (
    id SERIAL PRIMARY KEY,
    service_id INTEGER NOT NULL REFERENCES service.tile_services(id) ON DELETE CASCADE,
    layer_name VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT,

    -- 图层类型
    layer_type VARCHAR(50) NOT NULL CHECK (layer_type IN ('dynamic', 'static')),

    -- 图层配置（JSONB）
    -- dynamic 图层示例:
    -- {
    --   "source": {
    --     "engine_id": 1,
    --     "schema": "public",
    --     "table": "roads",
    --     "geometry_column": "geom",
    --     "srid": 4326
    --   },
    --   "mvt": {
    --     "extent": 1024,
    --     "buffer": 64
    --   },
    --   "cache": {
    --     "enabled": true,
    --     "ttl": 3600
    --   }
    -- }
    --
    -- static 图层示例:
    -- {
    --   "source": "external",
    --   "tile_path": "s3://bucket/tiles/basemap",
    --   "format": "png",
    --   "zoom_range": [0, 18]
    -- }
    layer_config JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- 显示配置
    display_order INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT TRUE,

    -- 审计字段
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- 唯一约束：同一服务下图层名称唯一
    CONSTRAINT unique_tile_layer_name UNIQUE (service_id, layer_name)
);

-- ============================================================================
-- 索引
-- ============================================================================

-- 瓦片服务表索引
CREATE INDEX IF NOT EXISTS idx_tile_services_tenant ON service.tile_services(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tile_services_status ON service.tile_services(status);
CREATE INDEX IF NOT EXISTS idx_tile_services_created_at ON service.tile_services(created_at DESC);

-- 瓦片服务图层表索引
CREATE INDEX IF NOT EXISTS idx_tile_service_layers_service ON service.tile_service_layers(service_id);
CREATE INDEX IF NOT EXISTS idx_tile_service_layers_enabled ON service.tile_service_layers(enabled);
CREATE INDEX IF NOT EXISTS idx_tile_service_layers_type ON service.tile_service_layers(layer_type);

-- GIN 索引用于 JSONB 查询
CREATE INDEX IF NOT EXISTS idx_tile_service_layers_config ON service.tile_service_layers USING GIN (layer_config);

-- ============================================================================
-- 注释
-- ============================================================================

COMMENT ON TABLE service.tile_services IS '瓦片服务主表，存储瓦片服务的基本配置';
COMMENT ON TABLE service.tile_service_layers IS '瓦片服务图层表，存储每个服务的图层配置';

COMMENT ON COLUMN service.tile_services.tenant_id IS '租户ID';
COMMENT ON COLUMN service.tile_services.service_name IS '服务名称（URL路径使用，同一租户下唯一）';
COMMENT ON COLUMN service.tile_services.title IS '服务标题（显示用）';
COMMENT ON COLUMN service.tile_services.default_srid IS '默认坐标系（3857=Web Mercator, 4326=WGS84）';
COMMENT ON COLUMN service.tile_services.extent IS '所有图层的联合边界（JSONB数组，格式: [minX, minY, maxX, maxY]）';
COMMENT ON COLUMN service.tile_services.protocols IS '支持的协议配置（XYZ Tiles, OGC Tiles, TMS等）';
COMMENT ON COLUMN service.tile_services.public_access IS '是否公开访问（true=不需要认证）';

COMMENT ON COLUMN service.tile_service_layers.layer_type IS '图层类型: dynamic（动态生成）或 static（静态瓦片）';
COMMENT ON COLUMN service.tile_service_layers.layer_config IS '图层配置（JSONB格式，根据layer_type有不同的结构）';
COMMENT ON COLUMN service.tile_service_layers.display_order IS '显示顺序（用于前端排序）';
COMMENT ON COLUMN service.tile_service_layers.enabled IS '是否启用（false时不对外提供服务）';

-- ============================================================================
-- 完成
-- ============================================================================

-- 验证表是否创建成功
DO $$
BEGIN
    IF EXISTS (
        SELECT FROM information_schema.tables
        WHERE table_schema = 'service'
        AND table_name = 'tile_services'
    ) AND EXISTS (
        SELECT FROM information_schema.tables
        WHERE table_schema = 'service'
        AND table_name = 'tile_service_layers'
    ) THEN
        RAISE NOTICE '✅ 瓦片服务表创建成功';
    ELSE
        RAISE EXCEPTION '❌ 瓦片服务表创建失败';
    END IF;
END $$;
