CREATE TABLE IF NOT EXISTS permissions (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    permission_key LONGTEXT NOT NULL,
    permission_key_digest BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(permission_key, 256))) STORED NOT NULL UNIQUE,
    owner_module LONGTEXT NOT NULL,
    action LONGTEXT NOT NULL,
    risk_level VARCHAR(16) NOT NULL,
    delegable BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_scope_types JSON NOT NULL,
    tenant_customizable BOOLEAN NOT NULL DEFAULT FALSE,
    name_i18n_key LONGTEXT NOT NULL,
    description_i18n_key LONGTEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CHECK (REGEXP_LIKE(permission_key, '^[a-z][a-z0-9_]*[.][a-z][a-z0-9_]*[.][a-z][a-z0-9_]*$', 'c')
           AND SUBSTRING_INDEX(permission_key, '.', -1) = action),
    CHECK (REGEXP_LIKE(owner_module, '^[a-z][a-z0-9_]*$', 'c')),
    CHECK (risk_level IN ('low','medium','high','critical') AND status IN ('active','disabled')),
    CHECK (delegable IN (0,1) AND tenant_customizable IN (0,1)),
    CHECK (TRIM(name_i18n_key) <> '' AND TRIM(description_i18n_key) <> ''),
    CHECK (JSON_SCHEMA_VALID('{"type":"array","minItems":1,"uniqueItems":true,"items":{"enum":["platform","tenant"]}}', allowed_scope_types))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
