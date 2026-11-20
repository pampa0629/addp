CREATE TABLE IF NOT EXISTS service_principals (
    id BIGINT NOT NULL PRIMARY KEY,
    required_principal_type VARCHAR(32) GENERATED ALWAYS AS ('service_principal') STORED NOT NULL,
    name LONGTEXT NOT NULL,
    description LONGTEXT NOT NULL DEFAULT (''),
    owner_scope VARCHAR(16) NOT NULL,
    owner_tenant_id BIGINT,
    owner_key BIGINT GENERATED ALWAYS AS (COALESCE(owner_tenant_id, 0)) STORED NOT NULL,
    name_digest BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(
        CASE WHEN owner_scope = 'tenant' THEN LOWER(TRIM(name)) ELSE name END, 256
    ))) STORED NOT NULL,
    created_by_principal_id BIGINT NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_service_principals_owner_name (owner_key, name_digest),
    CONSTRAINT service_principals_type_fk FOREIGN KEY (id, required_principal_type) REFERENCES principals (id, principal_type),
    CONSTRAINT service_principals_tenant_fk FOREIGN KEY (owner_tenant_id) REFERENCES tenants (id),
    CONSTRAINT service_principals_creator_fk FOREIGN KEY (created_by_principal_id) REFERENCES principals (id),
    CONSTRAINT service_principals_name_check CHECK (CHAR_LENGTH(TRIM(name)) BETWEEN 1 AND 120),
    CONSTRAINT service_principals_description_check CHECK (CHAR_LENGTH(description) <= 500),
    CONSTRAINT service_principals_version_check CHECK (version > 0),
    CONSTRAINT service_principals_owner_check CHECK (
        (owner_scope = 'platform' AND owner_tenant_id IS NULL) OR (owner_scope = 'tenant' AND owner_tenant_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
