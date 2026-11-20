CREATE TABLE IF NOT EXISTS module_discovery_snapshots (
    module_name LONGTEXT NOT NULL,
    module_key BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(module_name,256))) STORED UNIQUE,
    binding_id LONGTEXT NOT NULL,
    endpoints JSON NOT NULL CHECK (JSON_TYPE(endpoints) = 'ARRAY')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
