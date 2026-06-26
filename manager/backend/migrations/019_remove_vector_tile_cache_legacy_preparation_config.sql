-- 019_remove_vector_tile_cache_legacy_preparation_config.sql
-- Clean break: tile cache generation no longer owns quick view preparation.
-- 3857 quick view optimization targets are created and managed only by
-- manager.vector_quick_view_target_tasks / manager.vector_quick_view_targets.

UPDATE manager.vector_tile_cache_tasks
SET config = config - 'preparation'
WHERE config ? 'preparation';
