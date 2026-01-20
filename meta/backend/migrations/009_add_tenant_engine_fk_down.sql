-- Rollback: 删除租户-引擎外键约束

ALTER TABLE metadata.meta_item DROP CONSTRAINT IF EXISTS fk_meta_item_tenant_engine;
ALTER TABLE metadata.meta_node DROP CONSTRAINT IF EXISTS fk_meta_node_tenant_engine;
DROP INDEX IF EXISTS system.idx_engines_tenant_engine_unique;
