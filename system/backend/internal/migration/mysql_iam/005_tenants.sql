CREATE TABLE IF NOT EXISTS tenants (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    code LONGTEXT NOT NULL,
    code_digest BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(code, 256))) STORED NOT NULL,
    name LONGTEXT NOT NULL,
    description LONGTEXT NOT NULL DEFAULT (''),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    initialized_at DATETIME(6),
    initialized_by_principal_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_tenants_code_digest (code_digest),
    CONSTRAINT tenants_initialized_by_fk FOREIGN KEY (initialized_by_principal_id) REFERENCES principals (id),
    CONSTRAINT tenants_initialization_fact_complete CHECK (
        (initialized_at IS NULL) = (initialized_by_principal_id IS NULL)
    ),
    CONSTRAINT tenants_code_check CHECK (code = LOWER(TRIM(code)) AND code <> ''),
    CONSTRAINT tenants_name_check CHECK (TRIM(name) <> ''),
    CONSTRAINT tenants_status_check CHECK (status IN ('active', 'suspended', 'closed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
