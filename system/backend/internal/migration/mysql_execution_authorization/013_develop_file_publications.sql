CREATE TABLE IF NOT EXISTS develop_file_publications (
    approval_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    operation_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    lease_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    prepare_admitted_at DATETIME(6) NOT NULL,
    released_at DATETIME(6),
    preparation_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin,
    FOREIGN KEY (approval_id) REFERENCES develop_file_create_approvals(id),
    CHECK (REGEXP_LIKE(operation_id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c')),
    CHECK (REGEXP_LIKE(request_digest,'^[0-9a-f]{64}$','c') AND REGEXP_LIKE(lease_digest,'^[0-9a-f]{64}$','c')),
    CHECK ((released_at IS NULL AND preparation_digest IS NULL) OR
           (released_at IS NOT NULL AND released_at>=prepare_admitted_at AND preparation_digest IS NOT NULL
            AND REGEXP_LIKE(preparation_digest,'^[0-9a-f]{64}$','c')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
