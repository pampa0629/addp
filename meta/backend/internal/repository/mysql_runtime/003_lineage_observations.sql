CREATE TABLE IF NOT EXISTS meta.lineage_observations (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    relation_kind VARCHAR(32) NOT NULL CHECK (relation_kind IN ('derive', 'reference', 'serve')),
    granularity VARCHAR(32) NOT NULL DEFAULT 'item' CHECK (granularity IN ('item', 'field')),
    source_item_id BIGINT,
    target_item_id BIGINT,
    service_id BIGINT,
    published_revision VARCHAR(128),
    execution_id VARCHAR(64),
    producer_module VARCHAR(64) NOT NULL,
    capture_method VARCHAR(32) NOT NULL CHECK (capture_method IN ('declared', 'runtime', 'parsed')),
    source_snapshot JSON NOT NULL,
    target_snapshot JSON,
    evidence JSON NOT NULL DEFAULT (JSON_OBJECT()),
    observed_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT lineage_observations_endpoint_shape CHECK (
        (relation_kind IN ('derive', 'reference') AND source_item_id IS NOT NULL AND target_item_id IS NOT NULL)
        OR (relation_kind = 'serve' AND source_item_id IS NOT NULL AND service_id IS NOT NULL AND published_revision IS NOT NULL)
    ),
    FOREIGN KEY (source_item_id) REFERENCES meta.meta_item(id) ON DELETE RESTRICT,
    FOREIGN KEY (target_item_id) REFERENCES meta.meta_item(id) ON DELETE RESTRICT,
    source_field_name LONGTEXT NOT NULL DEFAULT (''),
    target_field_name LONGTEXT NOT NULL DEFAULT (''),
    source_schema_hash LONGTEXT NOT NULL DEFAULT (''),
    target_schema_hash LONGTEXT NOT NULL DEFAULT (''),
    field_execution BINARY(32) GENERATED ALWAYS AS (
        IF(granularity='field' AND execution_id IS NOT NULL AND source_item_id IS NOT NULL AND target_item_id IS NOT NULL,UNHEX(SHA2(CAST(JSON_ARRAY(tenant_id,execution_id,source_item_id,source_field_name,source_schema_hash,target_item_id,target_field_name,target_schema_hash,relation_kind) AS CHAR CHARACTER SET utf8mb4),256)),NULL)
    ) STORED,
    UNIQUE KEY uq_lineage_field_observation (field_execution),
    KEY idx_lineage_observations_source (tenant_id,source_item_id,observed_at DESC,id DESC),
    KEY idx_lineage_observations_target (tenant_id,target_item_id,observed_at DESC,id DESC),
    KEY idx_lineage_observations_execution (tenant_id,execution_id),
    KEY idx_lineage_observations_service (tenant_id,service_id,published_revision)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
