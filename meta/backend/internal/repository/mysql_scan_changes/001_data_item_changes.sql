CREATE TABLE IF NOT EXISTS meta.data_item_changes (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    item_id BIGINT NOT NULL,
    source_identity VARCHAR(64) NOT NULL,
    operation VARCHAR(16) NOT NULL CHECK (operation IN ('upsert','missing')),
    snapshot JSON NOT NULL,
    observed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_data_item_changes_tenant_cursor (tenant_id,id),
    KEY idx_data_item_changes_source (tenant_id,source_identity,id DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
