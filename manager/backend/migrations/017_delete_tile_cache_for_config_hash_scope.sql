-- 017_delete_tile_cache_for_config_hash_scope.sql
-- Clean break: config_hash now represents generation parameters only.
-- Existing tile cache rows used the previous broader hash scope and must not be reused.

DELETE FROM manager.tile_cache;
