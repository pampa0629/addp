CREATE TABLE IF NOT EXISTS manager.inference_scenario_bindings (
    id bigint unsigned AUTO_INCREMENT PRIMARY KEY,
    scenario_code varchar(80) NOT NULL, scope_type varchar(16) NOT NULL,
    tenant_id bigint unsigned, model_profile_id char(36) NOT NULL,
    version bigint unsigned NOT NULL, updated_by bigint unsigned NOT NULL,
    created_at datetime(6), updated_at datetime(6),
    scope_tenant bigint unsigned GENERATED ALWAYS AS (COALESCE(tenant_id,0)) VIRTUAL,
    UNIQUE KEY uq_manager_inference_binding (scenario_code, scope_type, scope_tenant),
    CHECK ((scope_type='platform' AND tenant_id IS NULL) OR (scope_type='tenant' AND tenant_id>0 AND tenant_id IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.embedding_configuration (
    id bigint unsigned PRIMARY KEY CHECK (id=1), version bigint unsigned NOT NULL,
    max_distance double NOT NULL, max_file_size_mb bigint NOT NULL,
    batch_concurrency bigint NOT NULL, updated_by bigint unsigned NOT NULL,
    created_at datetime(6), updated_at datetime(6)
) ENGINE=InnoDB;
