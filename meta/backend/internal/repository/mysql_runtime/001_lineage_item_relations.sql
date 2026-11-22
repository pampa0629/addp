CREATE TABLE IF NOT EXISTS meta.lineage_item_relations (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    source_item_id BIGINT NOT NULL,
    target_item_id BIGINT NOT NULL,
    relation_kind VARCHAR(32) NOT NULL CHECK (relation_kind IN ('derive', 'reference')),
    granularity VARCHAR(32) NOT NULL DEFAULT 'item' CHECK (granularity IN ('item', 'field')),
    write_mode VARCHAR(32),
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'unverified', 'stale', 'closed')),
    first_observed_at DATETIME(6) NOT NULL,
    last_observed_at DATETIME(6) NOT NULL,
    closed_at DATETIME(6),
    closed_by_observation_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    transformation VARCHAR(16) NOT NULL DEFAULT '',
    FOREIGN KEY (source_item_id) REFERENCES meta.meta_item(id) ON DELETE RESTRICT,
    FOREIGN KEY (target_item_id) REFERENCES meta.meta_item(id) ON DELETE RESTRICT,
    source_field_name LONGTEXT NOT NULL DEFAULT (''),
    target_field_name LONGTEXT NOT NULL DEFAULT (''),
    source_schema_hash LONGTEXT NOT NULL DEFAULT (''),
    target_schema_hash LONGTEXT NOT NULL DEFAULT (''),
    CONSTRAINT lineage_relation_endpoints CHECK (
        (granularity='item' AND source_item_id<>target_item_id AND source_field_name='' AND target_field_name=''
            AND source_schema_hash='' AND target_schema_hash='' AND transformation='')
        OR (granularity='field' AND source_field_name<>'' AND target_field_name<>'' AND source_schema_hash<>''
            AND target_schema_hash<>'' AND transformation IN ('direct','derived'))
    ),
    active_relation BINARY(32) GENERATED ALWAYS AS (
        IF(status='closed',NULL,UNHEX(SHA2(CAST(JSON_ARRAY(tenant_id,source_item_id,target_item_id,relation_kind,granularity,source_field_name,target_field_name,source_schema_hash,target_schema_hash) AS CHAR CHARACTER SET utf8mb4),256)))
    ) STORED,
    UNIQUE KEY uq_lineage_active_relation (active_relation),
    KEY idx_lineage_relations_source (tenant_id,source_item_id,status),
    KEY idx_lineage_relations_target (tenant_id,target_item_id,status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
