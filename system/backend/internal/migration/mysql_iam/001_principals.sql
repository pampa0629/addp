CREATE TABLE IF NOT EXISTS principals (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    principal_type VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    authorization_version BIGINT NOT NULL DEFAULT 1,
    deactivated_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_principals_id_type (id, principal_type),
    CONSTRAINT principals_type_check CHECK (principal_type IN ('user', 'service_principal')),
    CONSTRAINT principals_status_check CHECK (status IN ('active', 'suspended', 'deactivated')),
    CONSTRAINT principals_version_check CHECK (authorization_version > 0),
    CONSTRAINT principals_deactivation_check CHECK ((status = 'deactivated') = (deactivated_at IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
