CREATE TABLE IF NOT EXISTS local_accounts (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    username LONGTEXT NOT NULL,
    normalized_username LONGTEXT NOT NULL,
    normalized_username_digest BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(normalized_username, 256))) STORED NOT NULL,
    password_hash LONGTEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    locked_until DATETIME(6),
    password_changed_at DATETIME(6) NOT NULL,
    last_authenticated_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_local_accounts_user (user_id),
    UNIQUE KEY uq_local_accounts_username_digest (normalized_username_digest),
    CONSTRAINT local_accounts_user_fk FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT local_accounts_username_check CHECK (TRIM(username) <> ''),
    CONSTRAINT local_accounts_normalized_username_check CHECK (
        normalized_username = TRIM(normalized_username) AND normalized_username <> ''
    ),
    CONSTRAINT local_accounts_hash_check CHECK (password_hash <> ''),
    CONSTRAINT local_accounts_status_check CHECK (status IN ('active', 'locked', 'disabled')),
    CONSTRAINT local_accounts_lock_check CHECK (status = 'locked' OR locked_until IS NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
