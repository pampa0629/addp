CREATE TABLE IF NOT EXISTS oauth_authorization_requests (
    id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY CHECK (REGEXP_LIKE(id,'^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$','c')),
    request_secret_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE
        CHECK (REGEXP_LIKE(request_secret_hash,'^[0-9a-f]{64}$','c')),
    client_id VARCHAR(128) NOT NULL,
    redirect_uri LONGTEXT NOT NULL CHECK (CHAR_LENGTH(TRIM(redirect_uri))>0),
    response_types JSON NOT NULL CHECK (response_types=JSON_ARRAY('code')),
    response_mode VARCHAR(16) NOT NULL CHECK (response_mode IN ('query','form_post')),
    requested_scopes JSON NOT NULL,
    requested_audiences JSON NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','cancelled')),
    principal_id BIGINT,
    context_type VARCHAR(16) CHECK (context_type IN ('platform','tenant')),
    tenant_membership_id BIGINT,
    issued_authorization_version BIGINT CHECK (issued_authorization_version>0),
    granted_scopes JSON,
    granted_audiences JSON,
    authentication_methods JSON,
    assurance_level VARCHAR(4) CHECK (assurance_level IN ('aal1','aal2','aal3')),
    authenticated_at DATETIME(6),
    requested_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id),
    FOREIGN KEY (principal_id) REFERENCES principals(id),
    FOREIGN KEY (tenant_membership_id) REFERENCES tenant_memberships(id),
    INDEX(status,expires_at),
    CHECK (requested_at<=created_at AND created_at<expires_at),
    CHECK (completed_at IS NULL OR completed_at BETWEEN created_at AND expires_at),
    CHECK (JSON_SCHEMA_VALID('{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}',requested_scopes)),
    CHECK (JSON_SCHEMA_VALID('{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}',requested_audiences)),
    CHECK (JSON_SCHEMA_VALID('{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}',granted_scopes)),
    CHECK (JSON_SCHEMA_VALID('{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}',granted_audiences)),
    CHECK (JSON_SCHEMA_VALID('{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}',authentication_methods)),
    CHECK ((status='approved' AND principal_id IS NOT NULL AND context_type IS NOT NULL
            AND issued_authorization_version IS NOT NULL AND granted_scopes IS NOT NULL
            AND granted_audiences IS NOT NULL AND authentication_methods IS NOT NULL AND assurance_level IS NOT NULL
            AND authenticated_at IS NOT NULL AND completed_at IS NOT NULL
            AND ((context_type='platform' AND tenant_membership_id IS NULL) OR (context_type='tenant' AND tenant_membership_id IS NOT NULL)))
        OR (status<>'approved' AND principal_id IS NULL AND context_type IS NULL AND tenant_membership_id IS NULL
            AND issued_authorization_version IS NULL AND granted_scopes IS NULL AND granted_audiences IS NULL
            AND authentication_methods IS NULL AND assurance_level IS NULL AND authenticated_at IS NULL
            AND ((status='pending')=(completed_at IS NULL))))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
