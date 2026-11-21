CREATE TABLE IF NOT EXISTS meta.meta_item (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    node_id BIGINT NOT NULL,
    item_type VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    full_name LONGTEXT,
    fingerprint VARCHAR(64) NOT NULL,
    row_count BIGINT,
    size_bytes BIGINT,
    data_updated_at DATETIME(6),
    scanned_at DATETIME(6),
    scanned_depth VARCHAR(10) DEFAULT 'none',
    attributes JSON,
    created_at DATETIME(6),
    deleted_at DATETIME(6),
    index_delete_operation_id VARCHAR(36) NOT NULL DEFAULT '',
    UNIQUE KEY uq_meta_item_fingerprint (fingerprint),
    KEY idx_meta_item_node (node_id),
    KEY idx_meta_item_engine_tenant (tenant_id, engine_id),
    CONSTRAINT fk_meta_item_tenant_engine FOREIGN KEY (tenant_id, engine_id)
        REFERENCES `system`.engines (tenant_id, id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
