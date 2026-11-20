CREATE TABLE IF NOT EXISTS host_nodes (
    node_id CHAR(36) NOT NULL PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL CHECK (CHAR_LENGTH(TRIM(display_name)) > 0),
    node_kind VARCHAR(20) NOT NULL CHECK (node_kind IN ('physical','virtual')),
    addresses JSON NOT NULL CHECK (JSON_TYPE(addresses)='ARRAY' AND JSON_LENGTH(addresses)<=32),
    enabled BOOLEAN NOT NULL CHECK (enabled IN (0,1)),
    allowed_module_bindings JSON NOT NULL CHECK (JSON_TYPE(allowed_module_bindings)='ARRAY' AND JSON_LENGTH(allowed_module_bindings)<=64),
    version BIGINT NOT NULL CHECK (version>0),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CHECK (REGEXP_LIKE(node_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')
        AND node_id <> '00000000-0000-0000-0000-000000000000')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
