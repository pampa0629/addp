CREATE TABLE IF NOT EXISTS cleanup_requests (
    request_id CHAR(36) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL CHECK (tenant_id>0),
    requested_by BIGINT NOT NULL CHECK (requested_by>0),
    parent_execution_id CHAR(36) NOT NULL UNIQUE,
    intent_digest VARCHAR(64) NOT NULL CHECK (REGEXP_LIKE(intent_digest,'^[a-f0-9]{64}$','c')),
    source_operation_id VARCHAR(128) NOT NULL DEFAULT '',
    action VARCHAR(16) NOT NULL CHECK (action IN ('scan','execute')),
    protocol TEXT NOT NULL CHECK (OCTET_LENGTH(protocol)<=16384 AND JSON_TYPE(CAST(protocol AS JSON))='OBJECT'),
    engine_confirmation TEXT NOT NULL DEFAULT ('') CHECK (OCTET_LENGTH(engine_confirmation)<=2048),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    cutoff MEDIUMTEXT CHECK (OCTET_LENGTH(cutoff)<=262144 AND JSON_TYPE(CAST(cutoff AS JSON))='OBJECT'),
    requested_scope TEXT NOT NULL CHECK (OCTET_LENGTH(requested_scope)<=512 AND JSON_TYPE(CAST(requested_scope AS JSON))='ARRAY'),
    CHECK (REGEXP_LIKE(request_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')),
    CHECK (REGEXP_LIKE(parent_execution_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')),
    UNIQUE KEY cleanup_requests_source_operation (tenant_id,(NULLIF(source_operation_id,'')),action),
    KEY cleanup_requests_history (tenant_id,created_at DESC,request_id DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
