BEGIN;

CREATE INDEX idx_module_runtime_instances_definition_role_updated
    ON system.module_runtime_instances (module_definition_id, role, updated_at DESC, id DESC);

CREATE INDEX idx_module_runtime_instances_definition_registered
    ON system.module_runtime_instances (module_definition_id, registered_at DESC, id DESC);

COMMIT;
