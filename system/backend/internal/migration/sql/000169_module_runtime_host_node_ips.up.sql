ALTER TABLE system.module_runtime_instances
    ADD COLUMN host_node_ips jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT module_runtime_host_node_ips_array CHECK (jsonb_typeof(host_node_ips) = 'array');

CREATE INDEX idx_module_runtime_host_node_ips
    ON system.module_runtime_instances USING gin (host_node_ips jsonb_path_ops);
