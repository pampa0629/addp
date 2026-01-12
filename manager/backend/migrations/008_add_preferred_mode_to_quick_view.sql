-- 新增 preferred_mode 字段到 quick_view 表
-- 用于存储用户偏好的地图显示模式（GeoJSON 或 MVT）

BEGIN;

-- 1. 添加字段
ALTER TABLE manager.quick_view
ADD COLUMN IF NOT EXISTS preferred_mode VARCHAR(20) DEFAULT 'mvt'
CHECK (preferred_mode IN ('geojson', 'mvt'));

-- 2. 添加注释
COMMENT ON COLUMN manager.quick_view.preferred_mode IS '用户偏好的显示模式：geojson（GeoJSON 模式）, mvt（矢量瓦片模式）';

-- 3. 为已完成的预缓存任务设置默认为 mvt
UPDATE manager.quick_view
SET preferred_mode = 'mvt'
WHERE status = 'completed' AND preferred_mode IS NULL;

-- 4. 为未完成的预缓存任务设置默认为 geojson
UPDATE manager.quick_view
SET preferred_mode = 'geojson'
WHERE status IN ('none', 'generating', 'failed', 'cancelled') AND preferred_mode IS NULL;

COMMIT;
