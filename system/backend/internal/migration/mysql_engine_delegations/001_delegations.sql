CREATE TABLE IF NOT EXISTS engine_access_delegations (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    tenant_membership_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    granted_by_principal_id BIGINT NOT NULL,
    granted_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at DATETIME(6) NOT NULL,
    grant_reason TEXT NOT NULL CHECK (CHAR_LENGTH(TRIM(grant_reason)) > 0 AND CHAR_LENGTH(grant_reason) <= 2000),
    revoked_by_principal_id BIGINT,
    revoked_at DATETIME(6),
    revoked_reason TEXT,
    FOREIGN KEY (tenant_id,engine_id) REFERENCES engines(tenant_id,id),
    FOREIGN KEY (tenant_id,tenant_membership_id) REFERENCES tenant_memberships(tenant_id,id),
    FOREIGN KEY (granted_by_principal_id) REFERENCES principals(id),
    FOREIGN KEY (revoked_by_principal_id) REFERENCES principals(id),
    CHECK (expires_at > granted_at),
    CHECK ((status='active' AND revoked_by_principal_id IS NULL AND revoked_at IS NULL AND revoked_reason IS NULL)
        OR (status='revoked' AND revoked_by_principal_id IS NOT NULL AND revoked_at IS NOT NULL
            AND revoked_reason IS NOT NULL AND CHAR_LENGTH(TRIM(revoked_reason)) > 0 AND CHAR_LENGTH(revoked_reason) <= 2000
            AND revoked_at >= granted_at AND revoked_at < expires_at)),
    KEY delegation_engine (tenant_id,engine_id,id DESC),
    KEY delegation_window (tenant_id,engine_id,tenant_membership_id,status,expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
