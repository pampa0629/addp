CREATE TABLE IF NOT EXISTS access_tokens (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    family_id BIGINT NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    current_family_id BIGINT GENERATED ALWAYS AS (CASE WHEN revoked_at IS NULL THEN family_id END) STORED UNIQUE,
    KEY idx_access_tokens_active_expiry (revoked_at, expires_at),
    CONSTRAINT access_tokens_family_fk FOREIGN KEY (family_id) REFERENCES refresh_token_families (id),
    CONSTRAINT access_tokens_hash_check CHECK (REGEXP_LIKE(token_hash, '^[0-9a-f]{64}$', 'c')),
    CONSTRAINT access_tokens_clock_check CHECK (expires_at > created_at AND (revoked_at IS NULL OR revoked_at >= created_at))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
