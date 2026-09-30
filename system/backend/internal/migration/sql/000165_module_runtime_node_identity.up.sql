ALTER TABLE system.module_runtime_instances
    ADD COLUMN host_node_name varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN runtime_hostname varchar(255) NOT NULL DEFAULT '';

CREATE INDEX idx_module_runtime_host_node_name ON system.module_runtime_instances (lower(host_node_name));
CREATE INDEX idx_module_runtime_hostname ON system.module_runtime_instances (lower(runtime_hostname));
