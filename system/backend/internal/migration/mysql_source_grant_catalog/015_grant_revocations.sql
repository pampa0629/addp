CREATE TABLE IF NOT EXISTS engine_access_grant_revocations (
    request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    revoked_by_principal_id BIGINT NOT NULL,
    revoked_by_membership_id BIGINT NOT NULL,
    revoked_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    reason TEXT NOT NULL CHECK (reason=TRIM(reason) AND CHAR_LENGTH(reason) BETWEEN 1 AND 2000),
    revoked_request_ids JSON NOT NULL,
    FOREIGN KEY (request_id) REFERENCES engine_access_grants(request_id),
    FOREIGN KEY (revoked_by_principal_id) REFERENCES principals(id),
    FOREIGN KEY (revoked_by_membership_id) REFERENCES tenant_memberships(id),
    CHECK (JSON_TYPE(revoked_request_ids)='ARRAY' AND JSON_CONTAINS(revoked_request_ids, JSON_QUOTE(request_id)))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
