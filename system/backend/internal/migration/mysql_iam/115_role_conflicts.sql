CREATE TABLE IF NOT EXISTS role_conflicts (
    role_id_low BIGINT NOT NULL,
    role_id_high BIGINT NOT NULL,
    reason LONGTEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (role_id_low,role_id_high),
    FOREIGN KEY (role_id_low) REFERENCES roles(id),
    FOREIGN KEY (role_id_high) REFERENCES roles(id),
    CHECK (role_id_low < role_id_high AND TRIM(reason) <> '')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
