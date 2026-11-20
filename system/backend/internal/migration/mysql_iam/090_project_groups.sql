CREATE TABLE IF NOT EXISTS project_groups (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    code LONGTEXT NOT NULL,
    code_digest BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(code,256))) STORED NOT NULL,
    name LONGTEXT NOT NULL,
    description LONGTEXT NOT NULL DEFAULT (''),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE (tenant_id,code_digest), UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    CHECK (code=LOWER(TRIM(code)) AND code<>'' AND TRIM(name)<>''),
    CHECK (status IN ('active','closed') AND version>0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
