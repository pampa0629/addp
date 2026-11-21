CREATE TABLE IF NOT EXISTS meta.object_index_deletions (
    fingerprint VARCHAR(64) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    operation_id VARCHAR(36) NOT NULL,
    confirmed BOOLEAN NOT NULL,
    restored BOOLEAN NOT NULL,
    KEY idx_object_index_deletions_operation (operation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
