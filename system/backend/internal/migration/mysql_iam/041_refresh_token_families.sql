CREATE TABLE IF NOT EXISTS refresh_token_families (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    protocol_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin UNIQUE,
    principal_id BIGINT NOT NULL,
    context_type VARCHAR(16) NOT NULL,
    tenant_membership_id BIGINT,
    issued_authorization_version BIGINT NOT NULL,
    client_id LONGTEXT NOT NULL,
    auth_type VARCHAR(16) NOT NULL,
    audiences JSON NOT NULL,
    scopes JSON NOT NULL,
    authentication_methods JSON NOT NULL,
    assurance_level VARCHAR(16) NOT NULL,
    authenticated_at DATETIME(6) NOT NULL,
    step_up_expires_at DATETIME(6),
    oidc_subject LONGTEXT,
    oidc_acr LONGTEXT,
    oidc_amr JSON,
    oidc_claims_schema_version SMALLINT,
    oidc_claims JSON,
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6),
    revoked_reason LONGTEXT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_token_families_principal_expiry (principal_id, revoked_at, expires_at),
    KEY idx_token_families_membership_expiry (tenant_membership_id, revoked_at, expires_at),
    CONSTRAINT token_families_principal_fk FOREIGN KEY (principal_id) REFERENCES principals (id),
    CONSTRAINT token_families_membership_fk FOREIGN KEY (tenant_membership_id) REFERENCES tenant_memberships (id),
    CONSTRAINT token_families_identity_check CHECK (
        issued_authorization_version > 0 AND TRIM(client_id) <> ''
        AND assurance_level IN ('aal1', 'aal2', 'aal3', 'not_applicable')
        AND (protocol_request_id IS NULL OR REGEXP_LIKE(
            protocol_request_id, '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$', 'c'
        ))
    ),
    CONSTRAINT token_families_context_check CHECK (
        (context_type = 'platform' AND tenant_membership_id IS NULL)
        OR (context_type = 'tenant' AND tenant_membership_id IS NOT NULL)
    ),
    CONSTRAINT token_families_auth_check CHECK (
        (auth_type = 'first_party' AND protocol_request_id IS NULL AND client_id = 'addp-web'
            AND audiences = JSON_ARRAY('addp.api') AND scopes = JSON_ARRAY())
        OR (auth_type = 'oauth' AND protocol_request_id IS NOT NULL AND client_id <> 'addp-web' AND JSON_LENGTH(scopes) > 0)
    ),
    CONSTRAINT token_families_audiences_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', audiences
    )),
    CONSTRAINT token_families_scopes_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', scopes
    )),
    CONSTRAINT token_families_methods_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', authentication_methods
    )),
    CONSTRAINT token_families_clocks_check CHECK (
        authenticated_at <= created_at AND expires_at > created_at
        AND (step_up_expires_at IS NULL OR (step_up_expires_at >= authenticated_at AND step_up_expires_at <= expires_at))
    ),
    CONSTRAINT token_families_revocation_check CHECK (
        (revoked_at IS NULL AND revoked_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revoked_at >= created_at AND revoked_reason IS NOT NULL AND TRIM(revoked_reason) <> '')
    ),
    CONSTRAINT token_families_oidc_check CHECK (
        (oidc_subject IS NULL AND oidc_acr IS NULL AND oidc_amr IS NULL AND oidc_claims_schema_version IS NULL AND oidc_claims IS NULL)
        OR (auth_type = 'oauth' AND oidc_subject IS NOT NULL AND TRIM(oidc_subject) <> ''
            AND oidc_acr IS NOT NULL AND TRIM(oidc_acr) <> ''
            AND oidc_amr IS NOT NULL AND JSON_SCHEMA_VALID(
                '{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', oidc_amr
            )
            AND oidc_claims_schema_version IS NOT NULL AND oidc_claims_schema_version > 0
            AND oidc_claims IS NOT NULL AND JSON_TYPE(oidc_claims) = 'OBJECT')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
