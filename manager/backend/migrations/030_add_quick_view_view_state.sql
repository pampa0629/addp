ALTER TABLE manager.quick_view
    ADD COLUMN IF NOT EXISTS view_state JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN manager.quick_view.view_state IS '快显/预览交互状态。顶层按显示模式分为 basic_preview 和 quick_view，模式内按渲染域保存 map 地图视口和 scene_3d 三维相机状态';
