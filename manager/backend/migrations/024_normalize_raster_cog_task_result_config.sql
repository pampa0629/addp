-- 024_normalize_raster_cog_task_result_config.sql
-- Clean break: raster COG task config uses "result" for target COG result settings.
-- Historical "artifact" keys are migrated once and then removed.

UPDATE manager.raster_cog_tasks
SET config = jsonb_set(config - 'artifact', '{result}', config->'artifact', true)
WHERE config ? 'artifact'
  AND NOT (config ? 'result');

UPDATE manager.raster_cog_tasks
SET config = config - 'artifact'
WHERE config ? 'artifact';
