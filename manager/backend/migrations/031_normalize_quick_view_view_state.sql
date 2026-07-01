WITH normalized AS (
    SELECT
        id,
        COALESCE(view_state->'map', '{}'::jsonb) AS flat_map_state,
        (
            COALESCE(view_state->'model_3d', '{}'::jsonb) ||
            COALESCE(view_state->'tiles_3d', '{}'::jsonb) ||
            COALESCE(view_state->'gaussian_splat', '{}'::jsonb)
        ) AS flat_scene_3d_state
    FROM manager.quick_view
)
UPDATE manager.quick_view AS qv
SET view_state = jsonb_build_object(
    'basic_preview',
    COALESCE(qv.view_state->'basic_preview', '{}'::jsonb),
    'quick_view',
    jsonb_strip_nulls(jsonb_build_object(
        'map',
        NULLIF(normalized.flat_map_state, '{}'::jsonb),
        'scene_3d',
        NULLIF(normalized.flat_scene_3d_state, '{}'::jsonb)
    )) || COALESCE(qv.view_state->'quick_view', '{}'::jsonb)
)
FROM normalized
WHERE qv.id = normalized.id
  AND (
      normalized.flat_map_state <> '{}'::jsonb
      OR normalized.flat_scene_3d_state <> '{}'::jsonb
      OR qv.view_state ? 'basic_preview'
      OR qv.view_state ? 'quick_view'
  );

COMMENT ON COLUMN manager.quick_view.view_state IS '快显/预览交互状态。顶层按显示模式分为 basic_preview 和 quick_view，模式内按渲染域保存 map 地图视口和 scene_3d 三维相机状态';
