CREATE TABLE IF NOT EXISTS resource_access_tickets (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    family_id BIGINT NOT NULL,
    owner LONGTEXT NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    active_owner_hash BINARY(32) GENERATED ALWAYS AS (
        CASE WHEN revoked_at IS NULL THEN UNHEX(SHA2(owner, 256)) END
    ) STORED,
    UNIQUE KEY uq_resource_ticket_active_owner (family_id, active_owner_hash),
    KEY idx_resource_ticket_owner_expiry (owner(128), revoked_at, expires_at),
    FOREIGN KEY (family_id) REFERENCES refresh_token_families (id),
    CHECK (REGEXP_LIKE(token_hash, '^[0-9a-f]{64}$', 'c')),
    CHECK (REGEXP_LIKE(owner, '^[a-z][a-z0-9_]*$', 'c')),
    CHECK (expires_at > created_at AND (revoked_at IS NULL OR revoked_at >= created_at))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
