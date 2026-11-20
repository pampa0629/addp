CREATE TABLE IF NOT EXISTS iam_bootstrap_state (
    singleton BOOLEAN NOT NULL DEFAULT TRUE PRIMARY KEY,
    status VARCHAR(16) NOT NULL,
    secret_hash VARCHAR(64) UNIQUE,
    prepared_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6),
    CHECK (singleton = TRUE),
    CHECK (secret_hash IS NULL OR REGEXP_LIKE(secret_hash, '^[0-9a-f]{64}$', 'c')),
    CHECK (expires_at > prepared_at),
    CHECK (
        (status = 'prepared' AND secret_hash IS NOT NULL AND completed_at IS NULL)
        OR (status = 'completed' AND secret_hash IS NULL AND completed_at IS NOT NULL AND completed_at >= prepared_at)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
