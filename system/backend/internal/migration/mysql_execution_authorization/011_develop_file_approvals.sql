CREATE TABLE IF NOT EXISTS develop_file_create_approvals (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    authorization_id BIGINT NOT NULL,
    target_index INT NOT NULL CHECK (target_index>=0),
    scope_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL CHECK (REGEXP_LIKE(scope_digest,'^[0-9a-f]{64}$','c')),
    approver_principal_id BIGINT NOT NULL,
    approver_membership_id BIGINT NOT NULL,
    approver_authorization_version BIGINT NOT NULL CHECK (approver_authorization_version>0),
    approved_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_develop_file_approval_target(authorization_id,target_index),
    FOREIGN KEY (authorization_id) REFERENCES execution_authorizations(id),
    FOREIGN KEY (approver_principal_id) REFERENCES principals(id),
    FOREIGN KEY (approver_membership_id) REFERENCES tenant_memberships(id),
    CHECK (expires_at>approved_at),
    CHECK (REGEXP_LIKE(id,'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$','c'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
