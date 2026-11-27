CREATE TABLE IF NOT EXISTS engine_deployments (
    deployment_id CHAR(36) PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL CHECK (CHAR_LENGTH(TRIM(display_name))>0),
    engine_type VARCHAR(50) NOT NULL,
    enabled BOOLEAN NOT NULL CHECK (enabled IN (0,1)),
    inventory JSON NOT NULL,
    declared_node_id CHAR(36) NOT NULL,
    version BIGINT NOT NULL CHECK (version>0),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CHECK (updated_at>=created_at),
    source_engine VARCHAR(128) GENERATED ALWAYS AS (inventory->>'$.source.docker_engine_id') STORED,
    source_project VARCHAR(16) GENERATED ALWAYS AS (inventory->>'$.source.project') STORED,
    source_service VARCHAR(32) GENERATED ALWAYS AS (inventory->>'$.services[0].service') STORED,
    UNIQUE KEY engine_deployment_source_unique(source_engine,source_project,source_service),
    FOREIGN KEY (declared_node_id) REFERENCES host_nodes(node_id),
    CHECK (REGEXP_LIKE(deployment_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')
           AND deployment_id<>'00000000-0000-0000-0000-000000000000'),
    CHECK (COALESCE(JSON_TYPE(inventory)='OBJECT' AND inventory->>'$.schema'='addp.engine-deployment-inventory/v1'
       AND inventory->>'$.source.type'='docker_compose' AND inventory->>'$.source.runtime' IN ('linux_docker','desktop_linux_vm')
       AND source_project IN ('addp-infra','business') AND REGEXP_LIKE(source_engine,'^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$','c')
       AND JSON_TYPE(inventory->'$.services')='ARRAY' AND JSON_LENGTH(inventory->'$.services')=1
       AND inventory->>'$.services[0].engine_type'=engine_type AND source_service=CASE engine_type
          WHEN 'postgresql' THEN 'postgres' WHEN 'redis' THEN 'redis' WHEN 'minio' THEN 'minio'
          WHEN 'falkordb' THEN 'falkordb' WHEN 'mysql' THEN 'mysql' WHEN 'neo4j' THEN 'neo4j'
          WHEN 'ftp' THEN 'ftp' WHEN 'spark' THEN 'spark-master' END, FALSE)=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
