CREATE TABLE IF NOT EXISTS refresh_tokens (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    family_id BIGINT NOT NULL,
    issued_access_token_id BIGINT NOT NULL UNIQUE,
    parent_token_id BIGINT UNIQUE,
    replaced_by_token_id BIGINT UNIQUE,
    expires_at DATETIME(6) NOT NULL,
    used_at DATETIME(6),
    reuse_detected_at DATETIME(6),
    revoked_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    current_family_id BIGINT GENERATED ALWAYS AS (
        CASE WHEN used_at IS NULL AND revoked_at IS NULL THEN family_id END
    ) STORED UNIQUE,
    KEY idx_refresh_tokens_active_expiry (used_at, revoked_at, expires_at),
    CONSTRAINT refresh_tokens_family_fk FOREIGN KEY (family_id) REFERENCES refresh_token_families (id),
    CONSTRAINT refresh_tokens_access_fk FOREIGN KEY (issued_access_token_id) REFERENCES access_tokens (id),
    CONSTRAINT refresh_tokens_parent_fk FOREIGN KEY (parent_token_id) REFERENCES refresh_tokens (id),
    CONSTRAINT refresh_tokens_replacement_fk FOREIGN KEY (replaced_by_token_id) REFERENCES refresh_tokens (id),
    CONSTRAINT refresh_tokens_hash_check CHECK (REGEXP_LIKE(token_hash, '^[0-9a-f]{64}$', 'c')),
    CONSTRAINT refresh_tokens_clock_check CHECK (
        expires_at > created_at AND (used_at IS NULL OR used_at >= created_at)
        AND (reuse_detected_at IS NULL OR (used_at IS NOT NULL AND reuse_detected_at >= used_at))
        AND (revoked_at IS NULL OR revoked_at >= created_at)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
