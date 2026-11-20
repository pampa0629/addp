CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id VARCHAR(128) NOT NULL PRIMARY KEY,
    display_name LONGTEXT NOT NULL,
    client_type VARCHAR(16) NOT NULL,
    client_secret_hash LONGTEXT,
    redirect_uris JSON NOT NULL,
    grant_types JSON NOT NULL,
    response_types JSON NOT NULL,
    allowed_scopes JSON NOT NULL,
    allowed_audiences JSON NOT NULL,
    token_endpoint_auth_method VARCHAR(32) NOT NULL,
    jwks_uri LONGTEXT,
    jwks JSON,
    request_uris JSON NOT NULL DEFAULT (JSON_ARRAY()),
    id_token_signed_response_alg LONGTEXT,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    service_principal_id BIGINT UNIQUE,
    owner_scope VARCHAR(16) NOT NULL DEFAULT 'platform',
    owner_tenant_id BIGINT,
    version BIGINT NOT NULL DEFAULT 1,
    created_by_principal_id BIGINT,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_oauth_clients_tenant_management (owner_tenant_id, status, updated_at DESC, client_id),
    CONSTRAINT oauth_clients_service_fk FOREIGN KEY (service_principal_id) REFERENCES service_principals (id),
    CONSTRAINT oauth_clients_tenant_fk FOREIGN KEY (owner_tenant_id) REFERENCES tenants (id),
    CONSTRAINT oauth_clients_creator_fk FOREIGN KEY (created_by_principal_id) REFERENCES principals (id),
    CONSTRAINT oauth_clients_identity_check CHECK (
        REGEXP_LIKE(client_id, '^[A-Za-z0-9][A-Za-z0-9._-]{1,127}$', 'c')
        AND TRIM(display_name) <> '' AND status IN ('active', 'disabled') AND version > 0
    ),
    CONSTRAINT oauth_clients_secret_check CHECK (client_secret_hash IS NULL OR TRIM(client_secret_hash) <> ''),
    CONSTRAINT oauth_clients_redirect_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', redirect_uris
    )),
    CONSTRAINT oauth_clients_grant_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","minItems":1,"uniqueItems":true,"items":{"enum":["authorization_code","client_credentials","refresh_token","urn:ietf:params:oauth:grant-type:device_code"]}}',
        grant_types
    )),
    CONSTRAINT oauth_clients_response_check CHECK (response_types IN (JSON_ARRAY(), JSON_ARRAY('code'))),
    CONSTRAINT oauth_clients_scope_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', allowed_scopes
    )),
    CONSTRAINT oauth_clients_audience_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","minItems":1,"uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', allowed_audiences
    )),
    CONSTRAINT oauth_clients_request_check CHECK (JSON_SCHEMA_VALID(
        '{"type":"array","uniqueItems":true,"items":{"type":"string","pattern":"[^ ]"}}', request_uris
    )),
    CONSTRAINT oauth_clients_jwks_check CHECK (
        (jwks_uri IS NULL OR TRIM(jwks_uri) <> '') AND (jwks IS NULL OR JSON_TYPE(jwks) = 'OBJECT')
        AND (jwks_uri IS NULL OR jwks IS NULL)
    ),
    CONSTRAINT oauth_clients_signing_check CHECK (id_token_signed_response_alg IS NULL OR TRIM(id_token_signed_response_alg) <> ''),
    CONSTRAINT oauth_clients_credential_check CHECK (
        (client_type = 'public' AND client_secret_hash IS NULL AND token_endpoint_auth_method = 'none')
        OR (client_type = 'confidential' AND token_endpoint_auth_method = 'client_secret_basic'
            AND (client_secret_hash IS NOT NULL OR status = 'disabled'))
        OR (client_type = 'confidential' AND token_endpoint_auth_method = 'private_key_jwt'
            AND client_secret_hash IS NULL AND (jwks_uri IS NOT NULL OR jwks IS NOT NULL))
    ),
    CONSTRAINT oauth_clients_service_shape_check CHECK (
        (service_principal_id IS NULL AND NOT JSON_CONTAINS(grant_types, '"client_credentials"'))
        OR (service_principal_id IS NOT NULL AND client_type = 'confidential'
            AND token_endpoint_auth_method = 'client_secret_basic' AND redirect_uris = JSON_ARRAY()
            AND grant_types = JSON_ARRAY('client_credentials') AND response_types = JSON_ARRAY()
            AND allowed_scopes = JSON_ARRAY('addp.api') AND allowed_audiences = JSON_ARRAY('addp.api'))
    ),
    CONSTRAINT oauth_clients_owner_check CHECK (
        (owner_scope = 'platform' AND owner_tenant_id IS NULL)
        OR (owner_scope = 'tenant' AND owner_tenant_id IS NOT NULL AND created_by_principal_id IS NOT NULL
            AND allowed_scopes = JSON_ARRAY('addp.api') AND allowed_audiences = JSON_ARRAY('addp.api')
            AND jwks_uri IS NULL AND jwks IS NULL AND request_uris = JSON_ARRAY() AND id_token_signed_response_alg IS NULL
            AND (
                (REGEXP_LIKE(client_id, '^addp_ext_[A-Za-z0-9_-]{16,64}$', 'c')
                    AND client_type = 'public' AND service_principal_id IS NULL AND JSON_LENGTH(redirect_uris) > 0
                    AND grant_types = JSON_ARRAY('authorization_code', 'refresh_token') AND response_types = JSON_ARRAY('code'))
                OR (REGEXP_LIKE(client_id, '^addp_svc_[A-Za-z0-9_-]{16,64}$', 'c')
                    AND client_secret_hash IS NOT NULL AND service_principal_id IS NOT NULL)
            )
        )
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
