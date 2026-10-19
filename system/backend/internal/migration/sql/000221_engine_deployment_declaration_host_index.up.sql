BEGIN;
CREATE INDEX engine_deployment_declaration_host_index
    ON system.engine_deployment_declarations(node_id);
COMMIT;
