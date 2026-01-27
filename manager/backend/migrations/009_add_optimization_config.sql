-- 添加 optimization_config 字段到 quick_view 表
-- 用于存储 MVT 瓦片优化配置

ALTER TABLE manager.quick_view
ADD COLUMN IF NOT EXISTS optimization_config JSONB DEFAULT '{
  "version": "2.0",
  "attribute_pruning": {
    "enabled": true,
    "zoom_threshold": 8
  },
  "tile_size_thresholds": {
    "no_optimization_mb": 2.0,
    "stop_optimization_mb": 5.0
  },
  "extent_optimization": {
    "blur_level": 1
  },
  "sampling": {
    "polygon_line": {
      "cumulative_size_ratio": 0.8,
      "max_feature_count_ratio": 0.6
    },
    "point": {
      "sample_ratio": 0.6
    }
  },
  "simplification": {
    "tolerance_multiplier": 4.0,
    "algorithm": "visvalingam"
  }
}'::jsonb;

-- 添加注释
COMMENT ON COLUMN manager.quick_view.optimization_config IS 'MVT瓦片优化配置（v2.0）：包含属性优化、Extent优化、对象采样、几何简化等参数';
