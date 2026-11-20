CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    principal_id BIGINT,
    principal_type VARCHAR(32),
    context_type VARCHAR(16),
    tenant_id BIGINT,
    event_name LONGTEXT NOT NULL,
    result VARCHAR(16) NOT NULL,
    risk_level VARCHAR(16) NOT NULL,
    module_name LONGTEXT NOT NULL,
    http_method VARCHAR(16),
    resource_path LONGTEXT,
    http_status INT,
    request_id LONGTEXT,
    ip_address VARCHAR(64),
    user_agent LONGTEXT,
    entity_type LONGTEXT,
    entity_id LONGTEXT,
    details JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_audit_logs_created (created_at DESC, id DESC),
    KEY idx_audit_logs_tenant_created (tenant_id, created_at DESC, id DESC),
    KEY idx_audit_logs_principal_created (principal_id, created_at DESC, id DESC),
    KEY idx_audit_logs_event_created (event_name(191), created_at DESC, id DESC),
    KEY idx_audit_logs_risk_created (risk_level, created_at DESC, id DESC),
    CONSTRAINT audit_logs_principal_fk FOREIGN KEY (principal_id) REFERENCES principals (id),
    CONSTRAINT audit_logs_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants (id),
    CONSTRAINT audit_logs_event_check CHECK (
        REGEXP_LIKE(event_name, '^[a-z][a-z0-9_]*([.][a-z][a-z0-9_]*)+$', 'c')
        AND REGEXP_LIKE(module_name, '^[a-z][a-z0-9_]*$', 'c')
        AND result IN ('succeeded', 'failed', 'denied', 'ignored')
        AND risk_level IN ('low', 'medium', 'high', 'critical')
    ),
    CONSTRAINT audit_logs_principal_check CHECK (
        (principal_id IS NULL) = (principal_type IS NULL)
        AND (principal_type IS NULL OR principal_type IN ('user', 'service_principal'))
    ),
    CONSTRAINT audit_logs_context_check CHECK (
        (context_type = 'tenant' AND context_type IS NOT NULL AND tenant_id IS NOT NULL)
        OR ((context_type IS NULL OR context_type = 'platform') AND tenant_id IS NULL)
    ),
    CONSTRAINT audit_logs_http_check CHECK (
        (http_method IS NULL AND resource_path IS NULL AND http_status IS NULL)
        OR (http_method IS NOT NULL AND http_method IN ('GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS')
            AND resource_path IS NOT NULL AND TRIM(resource_path) <> ''
            AND http_status IS NOT NULL AND http_status BETWEEN 100 AND 599)
    ),
    CONSTRAINT audit_logs_metadata_check CHECK (
        (request_id IS NULL OR TRIM(request_id) <> '')
        AND (entity_type IS NULL OR TRIM(entity_type) <> '')
        AND (entity_id IS NULL OR TRIM(entity_id) <> '')
        AND (ip_address IS NULL OR INET6_ATON(ip_address) IS NOT NULL)
    ),
    CONSTRAINT audit_logs_details_check CHECK (JSON_TYPE(details) = 'OBJECT'),
    CONSTRAINT audit_logs_oauth_check CHECK (
        entity_type IS NULL OR entity_type <> 'oauth_security_event'
        OR (entity_id IS NOT NULL AND entity_id = event_name AND module_name = 'system'
            AND JSON_SCHEMA_VALID(
                '{"type":"object","additionalProperties":false,"properties":{"client_id":{"type":["string","null"]},"grant_type":{"type":["string","null"]},"decision":{"type":["string","null"]},"scope":{"type":["string","null"]},"error_code":{"type":["string","null"]}}}',
                details
            ))
    ),
    CONSTRAINT audit_logs_reuse_risk_check CHECK (
        event_name <> 'oauth.token.refresh_reuse_detected' OR risk_level IN ('high', 'critical')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
