-- Directory statistics have one owner: read-time aggregation of active Meta items.
-- Keep scan state; remove redundant scan-time quantities permanently.
ALTER TABLE meta.meta_node DROP COLUMN IF EXISTS item_count;
ALTER TABLE meta.meta_node DROP COLUMN IF EXISTS total_size_bytes;

CREATE INDEX IF NOT EXISTS idx_meta_node_active_parent
    ON meta.meta_node (parent_node_id, tenant_id, engine_id) INCLUDE (id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_meta_item_active_node
    ON meta.meta_item (tenant_id, engine_id, node_id) INCLUDE (id, size_bytes) WHERE deleted_at IS NULL;
