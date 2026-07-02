-- 023_drop_legacy_quick_view_tables.sql
-- 清理空间快显体系统一命名前遗留的 Manager 表。
-- 当前唯一保留的表：
--   manager.preview_state
--   manager.vector_materialized_view
--   manager.vector_materialized_view_tasks
--   manager.vector_tile_cache
--   manager.vector_tile_cache_tasks
--   manager.raster_cog
--   manager.raster_cog_tasks

DROP TABLE IF EXISTS manager.tile_cache_tasks;
DROP TABLE IF EXISTS manager.tile_cache;
DROP TABLE IF EXISTS manager.cog_artifact_tasks;
DROP TABLE IF EXISTS manager.cog_artifacts;
DROP TABLE IF EXISTS manager.vector_tile_cache_artifacts;
DROP TABLE IF EXISTS manager.mvt_tasks;
