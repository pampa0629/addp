-- 添加 extent_srid 字段修复 MinZoom 计算 Bug
-- 问题：Meta 扫描时已将 extent 转换为 WGS84，但保留了原始 SRID 标记
--       导致 Manager 模块误以为 extent 还是投影坐标，进行了二次转换
-- 解决：增加 extent_srid 字段，明确标识 extent 的实际坐标系

-- ============================================
-- 1. 更新 metadata.meta_item 表
-- ============================================

-- 为所有已存储的 spatial_metadata 添加 extent_srid 字段（默认 4326）
UPDATE metadata.meta_item
SET attributes = jsonb_set(
    attributes,
    '{spatial_metadata, extent_srid}',
    '4326'::jsonb
)
WHERE attributes ? 'spatial_metadata'
  AND attributes->'spatial_metadata' ? 'extent'
  AND NOT (attributes->'spatial_metadata' ? 'extent_srid');

-- 验证 Meta 数据更新结果
SELECT
    id,
    name,
    item_type,
    attributes->'spatial_metadata'->>'srid' as table_srid,
    attributes->'spatial_metadata'->>'extent_srid' as extent_srid,
    attributes->'spatial_metadata'->'extent' as extent
FROM metadata.meta_item
WHERE attributes ? 'spatial_metadata'
  AND item_type = 'table'
ORDER BY id DESC
LIMIT 10;

-- ============================================
-- 2. 更新 manager.quick_view 表
-- ============================================

-- 添加 extent_srid 列（如果不存在）
ALTER TABLE manager.quick_view
ADD COLUMN IF NOT EXISTS extent_srid INT DEFAULT 4326;

-- 更新已有 quick_view 记录（设置为 4326）
UPDATE manager.quick_view
SET extent_srid = 4326
WHERE extent_srid IS NULL OR extent_srid = 0;

-- 验证 QuickView 数据更新结果
SELECT
    id,
    table_name,
    status,
    min_zoom,
    max_zoom,
    extent_srid,
    extent
FROM manager.quick_view
ORDER BY id DESC
LIMIT 10;

-- ============================================
-- 3. 数据一致性检查
-- ============================================

-- 检查是否有 spatial_metadata 但缺少 extent_srid 的记录
SELECT
    COUNT(*) as missing_extent_srid_count
FROM metadata.meta_item
WHERE attributes ? 'spatial_metadata'
  AND attributes->'spatial_metadata' ? 'extent'
  AND NOT (attributes->'spatial_metadata' ? 'extent_srid');

-- 检查是否有 extent 但 extent_srid 为空的 quick_view 记录
SELECT
    COUNT(*) as missing_extent_srid_count
FROM manager.quick_view
WHERE extent IS NOT NULL
  AND jsonb_array_length(extent::jsonb) = 4
  AND (extent_srid IS NULL OR extent_srid = 0);

-- ============================================
-- 完成
-- ============================================
\echo '✅ 迁移完成！'
\echo '📊 建议：删除 dltb 的 quick_view 记录并重新触发快显，验证 min_zoom 修复'
\echo '   DELETE FROM manager.quick_view WHERE table_name = '\''dltb'\'';'
