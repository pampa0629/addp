CREATE TABLE IF NOT EXISTS module_definitions (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    module_name VARCHAR(50) NOT NULL UNIQUE,
    route_prefix VARCHAR(50) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    configuration_management JSON,
    task_provider JSON,
    declaration_published_at DATETIME(6),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CHECK (enabled IN (0,1) AND (module_name NOT IN ('system','gateway') OR enabled = 1)),
    INDEX (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
