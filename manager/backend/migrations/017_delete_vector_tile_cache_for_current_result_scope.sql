-- 017_delete_vector_tile_cache_for_current_result_scope.sql
-- Clean break: tile cache now keeps a single current result per item and format.
-- Existing tile cache rows used the previous configuration-scoped result identity and must not be reused.

DELETE FROM manager.vector_tile_cache;
