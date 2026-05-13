-- Rollback: 删除 meta_node 语义路径唯一约束

DROP INDEX IF EXISTS metadata.idx_meta_node_unique_full_name;
