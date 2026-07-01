-- 015_enforce_vector_tile_cache_resource_locator.sql
-- Clean break: tile cache and preview state locators use ResourceLocator only.


DELETE FROM manager.vector_tile_cache
WHERE locator IS NULL
   OR locator = ''
   OR locator NOT LIKE 'addp://engine/%/path/%?type=table%'
   OR locator NOT LIKE '%item_id=%'
   OR COALESCE(item_fingerprint, '') = ''
   OR item_fingerprint LIKE 'locator:%';

DELETE FROM manager.preview_state
WHERE COALESCE(item_fingerprint, '') = ''
   OR item_fingerprint LIKE 'locator:%'
   OR COALESCE(locator, '') = ''
   OR locator NOT LIKE 'addp://engine/%/path/%'
   OR locator NOT LIKE '%item_id=%';

DELETE FROM manager.vector_tile_cache_tasks
WHERE COALESCE(config #>> '{target,locator}', '') <> ''
  AND COALESCE(config #>> '{target,locator}', '') NOT LIKE 'addp://engine/%/path/%?type=table%';
