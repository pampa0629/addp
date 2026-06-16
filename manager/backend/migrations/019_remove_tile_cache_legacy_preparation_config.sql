-- 019_remove_tile_cache_legacy_preparation_config.sql
-- Clean break: tile cache generation no longer owns quick view preparation.
-- 3857 quick view optimization targets are created and managed only by
-- manager.quick_view_optimization_tasks / manager.quick_view_optimization.

UPDATE manager.tile_cache_tasks
SET config = config - 'preparation'
WHERE config ? 'preparation';
