CREATE TABLE IF NOT EXISTS runtime_metrics_leases (
    engine_id BIGINT NOT NULL PRIMARY KEY,
    instance_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    started_at DATETIME(6) NOT NULL,
    endpoint VARCHAR(512) NOT NULL,
    lease_expires_at DATETIME(6) NOT NULL,
    FOREIGN KEY (engine_id) REFERENCES engines(id) ON DELETE CASCADE,
    CHECK (REGEXP_LIKE(instance_id, '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')),
    CHECK (lease_expires_at > started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
