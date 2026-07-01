WITH normalized AS (
    SELECT
        id,
        CASE
            WHEN jsonb_typeof(view_state->'basic_preview') = 'object' THEN view_state->'basic_preview'
            ELSE '{}'::jsonb
        END AS basic_state,
        CASE
            WHEN jsonb_typeof(view_state->'quick_view') = 'object' THEN view_state->'quick_view'
            ELSE '{}'::jsonb
        END AS quick_state,
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
    jsonb_strip_nulls(jsonb_build_object(
        'map',
        NULLIF(normalized.basic_state->'map', '{}'::jsonb),
        'scene_3d',
        NULLIF((
            COALESCE(normalized.basic_state->'model_3d', '{}'::jsonb) ||
            COALESCE(normalized.basic_state->'tiles_3d', '{}'::jsonb) ||
            COALESCE(normalized.basic_state->'gaussian_splat', '{}'::jsonb) ||
            COALESCE(normalized.basic_state->'scene_3d', '{}'::jsonb)
        ), '{}'::jsonb)
    )),
    'quick_view',
    jsonb_strip_nulls(jsonb_build_object(
        'map',
        NULLIF((
            normalized.flat_map_state ||
            COALESCE(normalized.quick_state->'map', '{}'::jsonb)
        ), '{}'::jsonb),
        'scene_3d',
        NULLIF((
            normalized.flat_scene_3d_state ||
            COALESCE(normalized.quick_state->'model_3d', '{}'::jsonb) ||
            COALESCE(normalized.quick_state->'tiles_3d', '{}'::jsonb) ||
            COALESCE(normalized.quick_state->'gaussian_splat', '{}'::jsonb) ||
            COALESCE(normalized.quick_state->'scene_3d', '{}'::jsonb)
        ), '{}'::jsonb)
    ))
)
FROM normalized
WHERE qv.id = normalized.id
  AND (
      qv.view_state ? 'map'
      OR qv.view_state ? 'model_3d'
      OR qv.view_state ? 'tiles_3d'
      OR qv.view_state ? 'gaussian_splat'
      OR normalized.basic_state ? 'model_3d'
      OR normalized.basic_state ? 'tiles_3d'
      OR normalized.basic_state ? 'gaussian_splat'
      OR normalized.quick_state ? 'model_3d'
      OR normalized.quick_state ? 'tiles_3d'
      OR normalized.quick_state ? 'gaussian_splat'
  );

COMMENT ON COLUMN manager.quick_view.view_state IS '快显/预览交互状态。顶层按显示模式分为 basic_preview 和 quick_view，模式内按渲染域保存 map 地图视口和 scene_3d 三维相机状态';
