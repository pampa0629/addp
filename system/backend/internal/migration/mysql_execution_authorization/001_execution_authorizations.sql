CREATE TABLE IF NOT EXISTS execution_authorizations (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    actor_principal_id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    tenant_membership_id BIGINT NOT NULL,
    issued_authorization_version BIGINT NOT NULL,
    source_type VARCHAR(24) NOT NULL,
    source_definition_id BIGINT,
    source_definition_version CHAR(64) CHARACTER SET ascii COLLATE ascii_bin,
    source_notebook_session_authorization_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    source_execution_attempt INT,
    source_execution_lease_token CHAR(36) CHARACTER SET ascii COLLATE ascii_bin,
    execution_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    audience VARCHAR(16) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    sealed_at DATETIME(6),
    revoked_at DATETIME(6),
    revoked_reason TEXT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    develop_file_business_deadline DATETIME(6),
    develop_file_scope JSON,
    cleanup_scope JSON,
    source_read_scope JSON,
    internal_task JSON,
    attempt_key INT GENERATED ALWAYS AS (COALESCE(source_execution_attempt, 0)) STORED,
    UNIQUE KEY uq_execution_authorizations_execution_attempt (audience, execution_id, attempt_key),
    KEY idx_execution_authorizations_actor_active (actor_principal_id, tenant_id, revoked_at, expires_at),
    KEY idx_execution_authorizations_membership_active (tenant_membership_id, revoked_at, expires_at),
    KEY idx_execution_authorizations_expiry_active (revoked_at, expires_at, id),
    FOREIGN KEY (actor_principal_id) REFERENCES principals (id),
    FOREIGN KEY (tenant_id, tenant_membership_id) REFERENCES tenant_memberships (tenant_id, id),
    CONSTRAINT ck_execution_authorization_audience CHECK (audience IN ('model','quality','develop','transfer','service','duckdb','spark')),
    CONSTRAINT ck_execution_authorization_version CHECK (issued_authorization_version > 0),
    CONSTRAINT ck_execution_authorization_identity CHECK (REGEXP_LIKE(execution_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')),
    CONSTRAINT ck_execution_authorization_source CHECK (
        (source_type='user' AND source_definition_id IS NULL AND source_definition_version IS NULL)
        OR (source_type='service_definition' AND source_definition_id IS NOT NULL AND source_definition_id>0
            AND source_definition_version IS NOT NULL AND REGEXP_LIKE(source_definition_version,'^[0-9a-f]{64}$','c'))
    ),
    CONSTRAINT ck_execution_authorization_lease CHECK (
        (source_execution_attempt IS NULL AND source_execution_lease_token IS NULL)
        OR (source_type='user' AND source_execution_attempt IS NOT NULL AND source_execution_attempt>0
            AND source_execution_lease_token IS NOT NULL
            AND REGEXP_LIKE(source_execution_lease_token,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c'))
    ),
    CONSTRAINT ck_execution_authorization_expiry CHECK (expires_at>created_at AND expires_at<=created_at+INTERVAL 1 HOUR),
    CONSTRAINT ck_execution_authorization_revocation CHECK (
        (revoked_at IS NULL AND revoked_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revoked_at>=created_at AND revoked_reason IS NOT NULL AND TRIM(revoked_reason)<>'')
    ),
    CONSTRAINT ck_execution_authorization_generic_scope CHECK (
        source_notebook_session_authorization_id IS NULL AND develop_file_business_deadline IS NULL
        AND develop_file_scope IS NULL AND cleanup_scope IS NULL AND source_read_scope IS NULL AND internal_task IS NULL
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
