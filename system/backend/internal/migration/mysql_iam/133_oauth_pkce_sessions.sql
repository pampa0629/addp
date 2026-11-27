CREATE TABLE IF NOT EXISTS oauth_pkce_sessions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    authorization_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    authorization_code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin UNIQUE
        CHECK (REGEXP_LIKE(authorization_code_hash, '^[0-9a-f]{64}$', 'c')),
    code_challenge TEXT NOT NULL CHECK (CHAR_LENGTH(TRIM(code_challenge)) > 0),
    code_challenge_method VARCHAR(8) NOT NULL CHECK (code_challenge_method = 'S256'),
    verified_at DATETIME(6),
    consumed_at DATETIME(6),
    expires_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    FOREIGN KEY (authorization_request_id) REFERENCES oauth_authorization_requests(id),
    CHECK (expires_at > created_at),
    CHECK (verified_at IS NULL OR (authorization_code_hash IS NOT NULL AND verified_at BETWEEN created_at AND expires_at)),
    CHECK (consumed_at IS NULL OR (verified_at IS NOT NULL AND consumed_at BETWEEN verified_at AND expires_at))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
