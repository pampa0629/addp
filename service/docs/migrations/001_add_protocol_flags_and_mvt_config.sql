-- Migration: 添加协议开关和MVT配置字段
-- Date: 2026-02-02
-- Description: 为服务发布功能添加REST Query API开关、公开访问标志和MVT瓦片配置

BEGIN;

-- ============================================================================
-- Part 1: 扩展 internal_services 表
-- ============================================================================

-- 添加 REST Query API 开关
ALTER TABLE service.internal_services
ADD COLUMN IF NOT EXISTS enabled_rest_query BOOLEAN DEFAULT TRUE;

COMMENT ON COLUMN service.internal_services.enabled_rest_query IS '启用简化REST查询API（不遵循OGC标准）';

-- 添加公开访问标志
ALTER TABLE service.internal_services
ADD COLUMN IF NOT EXISTS public_access BOOLEAN DEFAULT FALSE;

COMMENT ON COLUMN service.internal_services.public_access IS '是否允许公开访问（无需JWT token）';

-- ============================================================================
-- Part 2: 扩展 internal_service_layers 表
-- ============================================================================

-- 修改 geometry_column 为 nullable（支持非空间数据）
ALTER TABLE service.internal_service_layers
ALTER COLUMN geometry_column DROP NOT NULL;

COMMENT ON COLUMN service.internal_service_layers.geometry_column IS '几何列名（NULL表示非空间数据）';

-- 添加 MVT 瓦片配置字段
ALTER TABLE service.internal_service_layers
ADD COLUMN IF NOT EXISTS mvt_buffer INTEGER DEFAULT 256;

COMMENT ON COLUMN service.internal_service_layers.mvt_buffer IS 'MVT瓦片缓冲区（像素），用于避免边界裁切问题';

ALTER TABLE service.internal_service_layers
ADD COLUMN IF NOT EXISTS mvt_extent INTEGER DEFAULT 4096;

COMMENT ON COLUMN service.internal_service_layers.mvt_extent IS 'MVT瓦片坐标范围，默认4096';

ALTER TABLE service.internal_service_layers
ADD COLUMN IF NOT EXISTS mvt_simplify_tolerance DOUBLE PRECISION;

COMMENT ON COLUMN service.internal_service_layers.mvt_simplify_tolerance IS '几何简化容差（米），根据缩放级别简化几何以减少传输量';

ALTER TABLE service.internal_service_layers
ADD COLUMN IF NOT EXISTS cache_control VARCHAR(50) DEFAULT 'public, max-age=3600';

COMMENT ON COLUMN service.internal_service_layers.cache_control IS 'HTTP缓存策略（Cache-Control头）';

-- ============================================================================
-- Part 3: 更新现有数据
-- ============================================================================

-- 为所有现有服务启用 REST Query API（默认行为）
UPDATE service.internal_services
SET enabled_rest_query = TRUE
WHERE enabled_rest_query IS NULL;

-- 为所有现有图层设置默认MVT配置
UPDATE service.internal_service_layers
SET
    mvt_buffer = 256,
    mvt_extent = 4096,
    cache_control = 'public, max-age=3600'
WHERE mvt_buffer IS NULL OR mvt_extent IS NULL;

-- ============================================================================
-- Part 4: 添加约束和验证
-- ============================================================================

-- 确保至少有一种服务类型启用（检查约束）
-- 注意：PostgreSQL的CHECK约束在行级别运行，这里添加一个提示
COMMENT ON TABLE service.internal_services IS '内部发布的OGC服务。注意：enabled_wfs, enabled_ogc_api, enabled_wmts, enabled_wms, enabled_rest_query 至少有一个为TRUE';

-- 为 geometry_column 为 NULL 的图层添加说明
COMMENT ON TABLE service.internal_service_layers IS '内部服务发布的图层。geometry_column 为 NULL 表示非空间数据，只能启用 REST Query API';

-- ============================================================================
-- Part 5: 索引优化（可选）
-- ============================================================================

-- 为常用查询添加索引
CREATE INDEX IF NOT EXISTS idx_internal_service_public_access
ON service.internal_services(public_access)
WHERE public_access = TRUE;

CREATE INDEX IF NOT EXISTS idx_internal_service_layer_geometry_null
ON service.internal_service_layers(service_id)
WHERE geometry_column IS NULL;

COMMIT;

-- ============================================================================
-- 回滚脚本（仅供参考，不执行）
-- ============================================================================

-- BEGIN;
-- ALTER TABLE service.internal_services DROP COLUMN IF EXISTS enabled_rest_query;
-- ALTER TABLE service.internal_services DROP COLUMN IF EXISTS public_access;
-- ALTER TABLE service.internal_service_layers DROP COLUMN IF EXISTS mvt_buffer;
-- ALTER TABLE service.internal_service_layers DROP COLUMN IF EXISTS mvt_extent;
-- ALTER TABLE service.internal_service_layers DROP COLUMN IF EXISTS mvt_simplify_tolerance;
-- ALTER TABLE service.internal_service_layers DROP COLUMN IF EXISTS cache_control;
-- ALTER TABLE service.internal_service_layers ALTER COLUMN geometry_column SET NOT NULL;
-- DROP INDEX IF EXISTS service.idx_internal_service_public_access;
-- DROP INDEX IF EXISTS service.idx_internal_service_layer_geometry_null;
-- COMMIT;
