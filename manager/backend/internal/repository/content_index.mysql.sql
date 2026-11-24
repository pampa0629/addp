CREATE TABLE IF NOT EXISTS manager.content_index_outlets (
    index_name varchar(128) PRIMARY KEY,
    epoch varchar(36) NOT NULL, endpoint_id varchar(64) NOT NULL,
    configured boolean NOT NULL, isolated boolean NOT NULL,
    cleanup_hold_id varchar(64) NOT NULL DEFAULT '',
    search_revision bigint NOT NULL DEFAULT 0, next_search_ordinal bigint NOT NULL DEFAULT 0,
    updated_at datetime(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.content_index_deliveries (
    id varchar(36) PRIMARY KEY,
    index_name varchar(128) NOT NULL, tenant_id bigint NOT NULL,
    cleanup_operation_id varchar(64) NOT NULL DEFAULT '',
    engine_ids json NOT NULL DEFAULT (JSON_ARRAY()),
    document_id varchar(128) NOT NULL, document_ids json NOT NULL DEFAULT (JSON_ARRAY()),
    kind varchar(16) NOT NULL, filter text NOT NULL,
    endpoint_id varchar(64) NOT NULL, task_correlation varchar(36) NOT NULL DEFAULT '',
    status varchar(16) NOT NULL, task_uid bigint, task_enqueued_at varchar(40) NOT NULL,
    created_at datetime(6), updated_at datetime(6),
    KEY idx_content_delivery_cleanup (cleanup_operation_id), KEY idx_content_delivery_tenant (tenant_id),
    KEY idx_content_delivery_pending (index_name, status),
    UNIQUE KEY idx_content_delivery_task (index_name, endpoint_id, task_uid),
    CONSTRAINT content_delivery_tenant CHECK (tenant_id > 0),
    CONSTRAINT content_delivery_kind CHECK (kind IN ('write','delete','purge')),
    CONSTRAINT content_delivery_correlation CHECK (task_correlation = '' OR task_correlation = id),
    CONSTRAINT content_delivery_status CHECK (status IN ('queued','submitting','submitted','unknown','succeeded','failed','canceled')),
    CONSTRAINT content_delivery_receipt CHECK (
        (status IN ('queued','submitting','unknown') AND task_uid IS NULL AND task_enqueued_at = '')
        OR (status IN ('submitted','succeeded','failed','canceled') AND task_uid IS NOT NULL AND task_uid >= 0 AND task_enqueued_at <> '')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.content_index_deletion_fences (
    index_name varchar(128) NOT NULL, tenant_id bigint NOT NULL,
    document_id varchar(128) NOT NULL, engine_id bigint unsigned NOT NULL, operation_id varchar(36) NOT NULL,
    PRIMARY KEY (index_name, tenant_id, document_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.cleanup_index_operations (
    id varchar(64) PRIMARY KEY, index_name varchar(128) NOT NULL, tenant_id bigint NOT NULL,
    execution_id varchar(64) NOT NULL UNIQUE, attempt bigint NOT NULL, request_digest varchar(64) NOT NULL,
    epoch varchar(64) NOT NULL, endpoint_id varchar(64) NOT NULL, inventory_digest varchar(64) NOT NULL,
    state varchar(16) NOT NULL, created_at datetime(6), settled_at datetime(6),
    KEY idx_cleanup_index_operations_index (index_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.cleanup_index_targets (
    index_name varchar(128) NOT NULL, tenant_id bigint NOT NULL, engine_id bigint unsigned NOT NULL,
    operation_id varchar(64) NOT NULL, PRIMARY KEY (index_name, tenant_id, engine_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.cleanup_index_assessments (
    request_id varchar(64) PRIMARY KEY, domain_digest varchar(64) NOT NULL, tenant_id bigint unsigned NOT NULL,
    requested_by bigint unsigned NOT NULL, scope_engine_id bigint unsigned NOT NULL,
    index_name varchar(128) NOT NULL, epoch varchar(64) NOT NULL, endpoint_id varchar(64) NOT NULL,
    inventory_digest varchar(64) NOT NULL, engine_ids json NOT NULL, candidate_count bigint NOT NULL,
    created_at datetime(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.search_document_ordinals (
    index_name varchar(128) NOT NULL, document_id varchar(128) NOT NULL,
    search_ordinal bigint NOT NULL CHECK (search_ordinal > 0),
    PRIMARY KEY (index_name, document_id), UNIQUE (index_name, search_ordinal)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.search_material_generation (
    singleton boolean PRIMARY KEY CHECK (singleton = TRUE),
    generation char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
) ENGINE=InnoDB;

INSERT INTO manager.search_material_generation (singleton, generation) VALUES (TRUE, UUID())
ON DUPLICATE KEY UPDATE singleton = TRUE;

CREATE TABLE IF NOT EXISTS manager.search_vector_revisions (
    tenant_id bigint PRIMARY KEY CHECK (tenant_id > 0),
    version bigint NOT NULL CHECK (version > 0)
) ENGINE=InnoDB;
