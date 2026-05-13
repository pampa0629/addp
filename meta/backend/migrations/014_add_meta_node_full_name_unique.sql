-- Migration: 添加 meta_node 语义路径唯一约束
-- Purpose: 防止同一资源路径因父节点或显示名变化被重复扫描成多套 node
-- Date: 2026-05-13

CREATE TEMP TABLE meta_node_duplicates_014 AS
WITH duplicates AS (
    SELECT
        id,
        path,
        ROW_NUMBER() OVER (
            PARTITION BY tenant_id, engine_id, node_type, full_name
            ORDER BY id DESC
        ) AS rn
    FROM metadata.meta_node
    WHERE deleted_at IS NULL AND full_name <> ''
)
SELECT id, path
FROM duplicates
WHERE rn > 1;

CREATE TEMP TABLE meta_node_delete_ids_014 AS
SELECT mn.id
FROM metadata.meta_node mn
JOIN meta_node_duplicates_014 dup
  ON mn.id = dup.id
  OR (dup.path <> '' AND mn.path LIKE dup.path || '/%');

DELETE FROM metadata.meta_item
WHERE node_id IN (
    SELECT id FROM meta_node_delete_ids_014
);

DELETE FROM metadata.meta_node
WHERE id IN (
    SELECT id FROM meta_node_delete_ids_014
);

DROP TABLE IF EXISTS meta_node_delete_ids_014;
DROP TABLE IF EXISTS meta_node_duplicates_014;

CREATE UNIQUE INDEX IF NOT EXISTS idx_meta_node_unique_full_name
ON metadata.meta_node (tenant_id, engine_id, node_type, full_name)
WHERE deleted_at IS NULL AND full_name <> '';
