CREATE TABLE IF NOT EXISTS tenant_invitations (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    email LONGTEXT NOT NULL,
    normalized_email LONGTEXT NOT NULL,
    secret_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    expires_at DATETIME(6) NOT NULL,
    accepted_at DATETIME(6),
    accepted_by_principal_id BIGINT,
    revoked_at DATETIME(6),
    revoked_by_principal_id BIGINT,
    expired_at DATETIME(6),
    created_by_principal_id BIGINT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    pending_email_digest BINARY(32) GENERATED ALWAYS AS (
        CASE WHEN status='pending' THEN UNHEX(SHA2(normalized_email,256)) END
    ) STORED,
    UNIQUE KEY uq_tenant_invitations_pending_email (tenant_id,pending_email_digest),
    KEY idx_tenant_invitations_tenant_status_created_at (tenant_id,status,created_at DESC),
    KEY idx_tenant_invitations_expires_at (status,expires_at),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (accepted_by_principal_id) REFERENCES principals(id),
    FOREIGN KEY (revoked_by_principal_id) REFERENCES principals(id),
    FOREIGN KEY (created_by_principal_id) REFERENCES principals(id),
    CHECK (TRIM(email)<>'' AND TRIM(normalized_email)<>''),
    CHECK (REGEXP_LIKE(secret_hash,'^[0-9a-f]{64}$','c')),
    CHECK (expires_at>created_at),
    CHECK (
        (status='pending' AND accepted_at IS NULL AND accepted_by_principal_id IS NULL
            AND revoked_at IS NULL AND revoked_by_principal_id IS NULL AND expired_at IS NULL)
        OR (status='accepted' AND accepted_at IS NOT NULL AND accepted_by_principal_id IS NOT NULL
            AND revoked_at IS NULL AND revoked_by_principal_id IS NULL AND expired_at IS NULL)
        OR (status='revoked' AND accepted_at IS NULL AND accepted_by_principal_id IS NULL
            AND revoked_at IS NOT NULL AND revoked_by_principal_id IS NOT NULL AND expired_at IS NULL)
        OR (status='expired' AND accepted_at IS NULL AND accepted_by_principal_id IS NULL
            AND revoked_at IS NULL AND revoked_by_principal_id IS NULL AND expired_at IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
