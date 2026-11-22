CREATE TABLE IF NOT EXISTS meta.meta_node (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    parent_node_id BIGINT,
    node_type VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    depth INT NOT NULL,
    path LONGTEXT,
    full_name LONGTEXT,
    scan_status VARCHAR(20) DEFAULT 'pending',
    scanned_depth VARCHAR(10) DEFAULT 'none',
    scanned_at DATETIME(6),
    scan_error LONGTEXT,
    attributes JSON,
    created_at DATETIME(6),
    deleted_at DATETIME(6),
    active_root TINYINT GENERATED ALWAYS AS (IF(deleted_at IS NULL AND parent_node_id IS NULL, 1, NULL)) STORED,
    active_parent BIGINT GENERATED ALWAYS AS (IF(deleted_at IS NULL, parent_node_id, NULL)) STORED,
    active_full_name BINARY(32) GENERATED ALWAYS AS
        (IF(deleted_at IS NULL AND OCTET_LENGTH(full_name) > 0, UNHEX(SHA2(full_name, 256)), NULL)) STORED,
    UNIQUE KEY uq_meta_node_root (tenant_id, engine_id, active_root, name, node_type),
    UNIQUE KEY uq_meta_node_child (tenant_id, engine_id, active_parent, name, node_type),
    UNIQUE KEY uq_meta_node_full_name (tenant_id, engine_id, node_type, active_full_name),
    KEY idx_meta_node_parent (parent_node_id),
    CONSTRAINT fk_meta_node_engine_restrict FOREIGN KEY (tenant_id, engine_id)
        REFERENCES `system`.engines (tenant_id, id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
