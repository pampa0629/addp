CREATE TABLE IF NOT EXISTS agent.inference_scenario_bindings (
    id BIGINT NOT NULL AUTO_INCREMENT,
    scenario_code VARCHAR(80) NOT NULL,
    scope_type VARCHAR(16) NOT NULL,
    tenant_id BIGINT,
    model_profile_id CHAR(32) NOT NULL,
    version BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    updated_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    CONSTRAINT ck_agent_inference_binding_scope CHECK (scope_type IN ('platform', 'tenant')),
    CONSTRAINT ck_agent_inference_binding_tenant CHECK ((scope_type = 'platform' AND tenant_id IS NULL) OR (scope_type = 'tenant' AND tenant_id IS NOT NULL)),
    platform_code VARCHAR(80) GENERATED ALWAYS AS (CASE WHEN scope_type='platform' THEN scenario_code END) STORED,
    UNIQUE KEY idx_agent_inference_binding_platform (platform_code),
    UNIQUE KEY idx_agent_inference_binding_tenant (scenario_code,tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
