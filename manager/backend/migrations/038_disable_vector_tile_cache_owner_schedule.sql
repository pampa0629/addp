-- 038_disable_vector_tile_cache_owner_schedule.sql
-- 瓦片缓存受管结果覆盖需要逐次显式确认，因此不再支持 owner 自身定时调度。

UPDATE manager.vector_tile_cache_tasks
SET schedule = '', next_run_at = NULL
WHERE COALESCE(schedule, '') <> '' OR next_run_at IS NOT NULL;

DROP INDEX IF EXISTS manager.idx_vector_tile_cache_tasks_schedule;
