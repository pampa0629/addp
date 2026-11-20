CREATE TABLE IF NOT EXISTS role_assignments (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    principal_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    scope_type VARCHAR(16) NOT NULL,
    tenant_id BIGINT,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    valid_from DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    valid_until DATETIME(6),
    source_type VARCHAR(16) NOT NULL,
    created_by_principal_id BIGINT,
    revoked_by_principal_id BIGINT,
    revoked_at DATETIME(6),
    grant_reason LONGTEXT,
    revoked_reason LONGTEXT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_role_assignments_principal_scope (principal_id, role_id, scope_type, tenant_id, status, valid_from),
    KEY idx_role_assignments_tenant_admin (tenant_id, scope_type, status, valid_until, valid_from),
    CONSTRAINT role_assignments_principal_fk FOREIGN KEY (principal_id) REFERENCES principals (id),
    CONSTRAINT role_assignments_role_fk FOREIGN KEY (role_id) REFERENCES roles (id),
    CONSTRAINT role_assignments_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants (id),
    CONSTRAINT role_assignments_creator_fk FOREIGN KEY (created_by_principal_id) REFERENCES principals (id),
    CONSTRAINT role_assignments_revoker_fk FOREIGN KEY (revoked_by_principal_id) REFERENCES principals (id),
    CONSTRAINT role_assignments_scope_check CHECK (
        (scope_type = 'platform' AND tenant_id IS NULL) OR (scope_type = 'tenant' AND tenant_id IS NOT NULL)
    ),
    CONSTRAINT role_assignments_validity_check CHECK (valid_until IS NULL OR valid_until > valid_from),
    CONSTRAINT role_assignments_source_check CHECK (source_type IN ('manual', 'idp_mapping', 'bootstrap', 'break_glass')),
    CONSTRAINT role_assignments_creator_check CHECK ((source_type = 'bootstrap') = (created_by_principal_id IS NULL)),
    CONSTRAINT role_assignments_grant_reason_check CHECK (grant_reason IS NULL OR TRIM(grant_reason) <> ''),
    CONSTRAINT role_assignments_lifecycle_check CHECK (
        (status = 'active' AND revoked_by_principal_id IS NULL AND revoked_at IS NULL AND revoked_reason IS NULL)
        OR (status = 'revoked' AND revoked_by_principal_id IS NOT NULL
            AND revoked_at IS NOT NULL AND revoked_reason IS NOT NULL AND TRIM(revoked_reason) <> '')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
