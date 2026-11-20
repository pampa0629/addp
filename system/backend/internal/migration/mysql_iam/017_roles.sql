CREATE TABLE IF NOT EXISTS roles (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT,
    role_key LONGTEXT NOT NULL,
    role_key_digest BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(role_key, 256))) STORED NOT NULL,
    tenant_key BIGINT GENERATED ALWAYS AS (COALESCE(tenant_id, 0)) STORED NOT NULL,
    name LONGTEXT,
    description LONGTEXT,
    name_i18n_key LONGTEXT,
    description_i18n_key LONGTEXT,
    role_type VARCHAR(16) NOT NULL,
    allowed_scope_types JSON NOT NULL,
    allowed_principal_types JSON NOT NULL,
    immutable BOOLEAN NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_by_principal_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_roles_tenant_role_key (tenant_key, role_key_digest),
    CONSTRAINT roles_tenant_fk FOREIGN KEY (tenant_id) REFERENCES tenants (id),
    CONSTRAINT roles_creator_fk FOREIGN KEY (created_by_principal_id) REFERENCES principals (id),
    CONSTRAINT roles_key_check CHECK (REGEXP_LIKE(role_key, '^[a-z][a-z0-9_]*[.][a-z][a-z0-9_]*$', 'c')),
    CONSTRAINT roles_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT roles_shape_check CHECK (
        (role_type IN ('platform_builtin', 'tenant_builtin') AND tenant_id IS NULL
         AND name IS NULL AND description IS NULL AND name_i18n_key IS NOT NULL
         AND description_i18n_key IS NOT NULL AND TRIM(name_i18n_key) <> '' AND TRIM(description_i18n_key) <> ''
         AND immutable = 1 AND created_by_principal_id IS NULL)
        OR (role_type = 'tenant_custom' AND tenant_id IS NOT NULL
         AND name IS NOT NULL AND TRIM(name) <> '' AND name_i18n_key IS NULL AND description_i18n_key IS NULL
         AND immutable = 0 AND created_by_principal_id IS NOT NULL)
    ),
    CONSTRAINT roles_scope_check CHECK (
        (role_type = 'platform_builtin' AND allowed_scope_types = JSON_ARRAY('platform'))
        OR (role_type IN ('tenant_builtin', 'tenant_custom') AND allowed_scope_types = JSON_ARRAY('tenant'))
    ),
    CONSTRAINT roles_principal_types_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","minItems":1,"uniqueItems":true,"items":{"enum":["user","service_principal"]}}',
        allowed_principal_types
    ))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
