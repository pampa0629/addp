CREATE TABLE IF NOT EXISTS manager.task_definitions (
    id bigint unsigned AUTO_INCREMENT PRIMARY KEY,
    tenant_id bigint unsigned NOT NULL,
    task_type varchar(80) NOT NULL,
    version bigint unsigned NOT NULL DEFAULT 1,
    name varchar(255) NOT NULL,
    description text, enabled boolean NOT NULL, schedule varchar(255),
    next_run_at datetime(6), last_run_at datetime(6),
    last_execution_id varchar(36), last_execution_status varchar(50),
    binding_status varchar(16) NOT NULL DEFAULT 'active',
    binding_issue varchar(32) NOT NULL DEFAULT '',
    semantic_key varchar(160), config json NOT NULL DEFAULT (JSON_OBJECT()),
    created_by bigint unsigned, created_at datetime(6) NOT NULL,
    updated_at datetime(6) NOT NULL, deleted_at datetime(6),
    active_semantic_key varchar(160) GENERATED ALWAYS AS
        (CASE WHEN deleted_at IS NULL THEN NULLIF(semantic_key,'') END) VIRTUAL,
    UNIQUE KEY uq_manager_task_definition_semantic (tenant_id,task_type,active_semantic_key),
    KEY idx_manager_task_definitions_tenant_type (tenant_id,task_type),
    KEY idx_manager_task_definitions_tenant_binding_status (tenant_id,binding_status),
    KEY idx_manager_task_definitions_last_execution (last_execution_id),
    KEY idx_manager_task_definitions_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS manager.task_resource_bindings (
    id bigint unsigned AUTO_INCREMENT PRIMARY KEY,
    task_definition_id bigint unsigned NOT NULL, tenant_id bigint unsigned NOT NULL,
    role varchar(16) NOT NULL, engine_id bigint unsigned NOT NULL, locator text NOT NULL,
    item_id bigint unsigned, item_fingerprint varchar(64), ordinal bigint NOT NULL DEFAULT 0,
    created_at datetime(6) NOT NULL,
    UNIQUE KEY uq_manager_task_resource_binding (task_definition_id,role,ordinal),
    KEY idx_manager_task_resource_engine (tenant_id,engine_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
