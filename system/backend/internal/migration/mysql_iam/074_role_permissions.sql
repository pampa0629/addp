CREATE TABLE IF NOT EXISTS role_permissions (
    role_id BIGINT NOT NULL,
    permission_id BIGINT NOT NULL,
    source_type VARCHAR(16) NOT NULL,
    created_by_principal_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (role_id) REFERENCES roles (id),
    FOREIGN KEY (permission_id) REFERENCES permissions (id),
    FOREIGN KEY (created_by_principal_id) REFERENCES principals (id),
    CHECK ((source_type = 'product' AND created_by_principal_id IS NULL)
        OR (source_type = 'tenant' AND created_by_principal_id IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
