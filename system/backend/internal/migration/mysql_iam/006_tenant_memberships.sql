CREATE TABLE IF NOT EXISTS tenant_memberships (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    principal_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    source_type VARCHAR(16) NOT NULL,
    source_ref LONGTEXT,
    joined_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6),
    ended_at DATETIME(6),
    created_by_principal_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_tenant_memberships_tenant_principal (tenant_id, principal_id),
    UNIQUE KEY uq_tenant_memberships_tenant_id (tenant_id, id),
    KEY idx_tenant_memberships_principal_status (principal_id, status),
    KEY idx_tenant_memberships_tenant_status_created (tenant_id, status, created_at),
    KEY idx_tenant_memberships_created_by (created_by_principal_id),
    CONSTRAINT tenant_memberships_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants (id),
    CONSTRAINT tenant_memberships_principal_fk FOREIGN KEY (principal_id) REFERENCES principals (id),
    CONSTRAINT tenant_memberships_created_by_fk FOREIGN KEY (created_by_principal_id) REFERENCES principals (id),
    CONSTRAINT tenant_memberships_status_check CHECK (status IN ('active', 'suspended', 'ended')),
    CONSTRAINT tenant_memberships_source_check CHECK (
        source_type IN ('manual', 'idp_jit', 'directory_sync', 'bootstrap', 'invitation')
    ),
    CONSTRAINT tenant_memberships_expiry_check CHECK (expires_at IS NULL OR expires_at > joined_at),
    CONSTRAINT tenant_memberships_ended_check CHECK ((status = 'ended') = (ended_at IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
