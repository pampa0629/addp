CREATE TABLE IF NOT EXISTS users (
    id BIGINT NOT NULL PRIMARY KEY,
    display_name LONGTEXT NOT NULL,
    primary_email LONGTEXT,
    locale LONGTEXT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    required_principal_type VARCHAR(32) GENERATED ALWAYS AS ('user') STORED NOT NULL,
    CONSTRAINT users_display_name_check CHECK (TRIM(display_name) <> ''),
    CONSTRAINT users_principal_type_fk FOREIGN KEY (id, required_principal_type)
        REFERENCES principals (id, principal_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
