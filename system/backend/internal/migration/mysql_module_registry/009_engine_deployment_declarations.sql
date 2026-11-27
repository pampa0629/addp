CREATE TABLE IF NOT EXISTS engine_deployment_declarations (
    deployment_id CHAR(36) NOT NULL,
    version BIGINT NOT NULL CHECK (version>0),
    node_id CHAR(36) NOT NULL,
    inventory JSON NOT NULL CHECK (JSON_TYPE(inventory)='OBJECT'),
    enabled BOOLEAN NOT NULL CHECK (enabled IN (0,1)),
    valid_from DATETIME(6) NOT NULL,
    valid_to DATETIME(6),
    CHECK (valid_to IS NULL OR valid_to>valid_from),
    current_deployment CHAR(36) GENERATED ALWAYS AS (IF(valid_to IS NULL,deployment_id,NULL)) STORED,
    PRIMARY KEY(deployment_id,version),
    UNIQUE KEY engine_deployment_current_declaration(current_deployment),
    FOREIGN KEY (deployment_id) REFERENCES engine_deployments(deployment_id),
    FOREIGN KEY (node_id) REFERENCES host_nodes(node_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
