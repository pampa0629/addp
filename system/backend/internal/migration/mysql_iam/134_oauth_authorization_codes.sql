CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE
        CHECK (REGEXP_LIKE(code_hash, '^[0-9a-f]{64}$', 'c')),
    authorization_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    expires_at DATETIME(6) NOT NULL,
    invalidated_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    FOREIGN KEY (authorization_request_id) REFERENCES oauth_authorization_requests(id),
    INDEX (invalidated_at, expires_at),
    CHECK (expires_at > created_at),
    CHECK (invalidated_at IS NULL OR invalidated_at BETWEEN created_at AND expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
