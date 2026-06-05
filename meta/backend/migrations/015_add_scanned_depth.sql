ALTER TABLE meta.meta_node
    ADD COLUMN IF NOT EXISTS scanned_depth VARCHAR(10) NOT NULL DEFAULT 'none';

ALTER TABLE meta.meta_item
    ADD COLUMN IF NOT EXISTS scanned_depth VARCHAR(10) NOT NULL DEFAULT 'none';

CREATE INDEX IF NOT EXISTS idx_meta_node_scanned_depth
    ON meta.meta_node(scanned_depth);

CREATE INDEX IF NOT EXISTS idx_meta_item_scanned_depth
    ON meta.meta_item(scanned_depth);
