CREATE TABLE IF NOT EXISTS provider_connections (
    id CHAR(36) CHARACTER SET ascii PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    scope_type VARCHAR(16) NOT NULL,
    tenant_id BIGINT UNSIGNED,
    adapter_type VARCHAR(40) NOT NULL,
    endpoint TEXT NOT NULL,
    allow_all_tenants BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    credential_ciphertext TEXT,
    credential_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_by BIGINT UNSIGNED NOT NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6),
    updated_at DATETIME(6),
    INDEX (scope_type),
    INDEX (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS provider_tenant_grants (
    provider_connection_id CHAR(36) CHARACTER SET ascii NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6),
    PRIMARY KEY (provider_connection_id, tenant_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS model_deployments (
    id CHAR(36) CHARACTER SET ascii PRIMARY KEY,
    provider_connection_id CHAR(36) CHARACTER SET ascii NOT NULL,
    name VARCHAR(120) NOT NULL,
    upstream_model VARCHAR(255) NOT NULL,
    operations JSON NOT NULL,
    modalities JSON NOT NULL,
    dimension BIGINT NOT NULL DEFAULT 0,
    chat_max_output_tokens_parameter VARCHAR(32) NOT NULL DEFAULT 'max_tokens',
    chat_temperature_mode VARCHAR(32) NOT NULL DEFAULT 'configurable',
    chat_thinking_mode VARCHAR(32) NOT NULL DEFAULT 'upstream_default',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_by BIGINT UNSIGNED NOT NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6),
    updated_at DATETIME(6),
    INDEX (provider_connection_id),
    CHECK (chat_max_output_tokens_parameter IN ('max_tokens', 'max_completion_tokens')),
    CHECK (chat_temperature_mode IN ('configurable', 'default_only'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS model_profiles (
    id CHAR(36) CHARACTER SET ascii PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    code VARCHAR(80) NOT NULL,
    scope_type VARCHAR(16) NOT NULL,
    tenant_id BIGINT UNSIGNED,
    model_deployment_id CHAR(36) CHARACTER SET ascii NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by BIGINT UNSIGNED NOT NULL,
    updated_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6),
    updated_at DATETIME(6),
    platform_code VARCHAR(80) GENERATED ALWAYS AS
        (CASE WHEN scope_type='platform' AND tenant_id IS NULL THEN code END) STORED,
    profile_tenant_id BIGINT UNSIGNED GENERATED ALWAYS AS
        (CASE WHEN scope_type='tenant' THEN tenant_id END) STORED,
    UNIQUE INDEX idx_inference_platform_profile_code (platform_code),
    UNIQUE INDEX idx_inference_tenant_profile_code (profile_tenant_id, code),
    INDEX idx_inference_profiles_resolution (scope_type, tenant_id, status),
    INDEX (model_deployment_id),
    INDEX (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS credential_audits (
    id CHAR(36) CHARACTER SET ascii PRIMARY KEY,
    provider_connection_id CHAR(36) CHARACTER SET ascii NOT NULL,
    old_version BIGINT UNSIGNED NOT NULL,
    new_version BIGINT UNSIGNED NOT NULL,
    action VARCHAR(16) NOT NULL,
    principal_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6),
    INDEX (provider_connection_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
