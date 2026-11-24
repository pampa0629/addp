CREATE TABLE IF NOT EXISTS orchestrator.orchestrations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512),
    steps JSON NOT NULL,
    editor_layout JSON NOT NULL DEFAULT (JSON_OBJECT()),
    enabled BOOLEAN DEFAULT FALSE,
    schedule VARCHAR(128),
    last_run_at DATETIME(6),
    next_run_at DATETIME(6),
    last_execution_id VARCHAR(36),
    last_execution_status VARCHAR(20),
    created_by BIGINT UNSIGNED,
    authorization_ref CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    authorization_subject_id BIGINT,
    authorization_definition_hash VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin,
    authorization_principal_id BIGINT,
    authorization_membership_id BIGINT,
    authorization_version BIGINT,
    authorized_at DATETIME(6),
    created_at DATETIME(6),
    updated_at DATETIME(6),
    deleted_at DATETIME(6),
    INDEX idx_orchestrations_tenant_id (tenant_id),
    INDEX idx_orchestrations_deleted_at (deleted_at),
    UNIQUE KEY uq_orchestrations_authorization_ref (authorization_ref),
    CHECK (enabled IN (FALSE, TRUE)),
    CHECK (authorization_ref IS NULL OR REGEXP_LIKE(authorization_ref,
        '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$', 'i')),
    CONSTRAINT orchestrations_authorization_subject_complete CHECK (
        (authorization_ref IS NULL AND authorization_subject_id IS NULL
         AND authorization_definition_hash IS NULL AND authorization_principal_id IS NULL
         AND authorization_membership_id IS NULL AND authorization_version IS NULL AND authorized_at IS NULL)
        OR
        (authorization_ref IS NOT NULL AND authorization_subject_id > 0
         AND REGEXP_LIKE(authorization_definition_hash, '^[0-9a-f]{64}$', 'c')
         AND authorization_principal_id > 0 AND authorization_membership_id > 0
         AND authorization_version > 0 AND authorized_at IS NOT NULL)
    ),
    CONSTRAINT orchestrations_enabled_authorization_subject CHECK (enabled = FALSE OR authorization_subject_id IS NOT NULL)
) ENGINE=InnoDB
