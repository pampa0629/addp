-- 020_normalize_tile_generation_target_metadata.sql
-- 补齐存量瓦片缓存 execution metadata 的 tile_generation_target.target_kind。
-- 当前前端和规范只消费 target_kind，不再保留 prepared_3857 作为展示判断主路径。

UPDATE common.task_executions
SET metadata = jsonb_set(
    metadata,
    '{tile_generation_target}',
    ((metadata->'tile_generation_target') - 'prepared_3857') || jsonb_build_object(
        'target_kind',
        CASE
            WHEN metadata->'tile_generation_target'->>'target_kind' IS NOT NULL
                THEN metadata->'tile_generation_target'->>'target_kind'
            WHEN metadata->'tile_generation_target'->>'prepared_3857' = 'true'
                AND (
                    metadata->'tile_generation_target'->>'table' LIKE 'addp_qvo_%'
                    OR metadata->'tile_generation_target'->>'table' LIKE 'addp_vmv_%'
                )
                THEN 'source_schema_materialized_view'
            WHEN metadata->'tile_generation_target'->>'prepared_3857' = 'true'
                THEN 'external_3857_materialized_view'
            ELSE 'source_table'
        END
    ),
    true
)
WHERE module = 'manager'
  AND task_type = 'vector_tile_cache_generation'
  AND metadata ? 'tile_generation_target'
  AND (
      NOT (metadata->'tile_generation_target' ? 'target_kind')
      OR metadata->'tile_generation_target' ? 'prepared_3857'
  );
