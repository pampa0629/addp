-- Rollback: 删除 meta_node 唯一性约束

DROP INDEX IF EXISTS meta.idx_meta_node_unique_with_parent;
DROP INDEX IF EXISTS meta.idx_meta_node_unique_without_parent;
