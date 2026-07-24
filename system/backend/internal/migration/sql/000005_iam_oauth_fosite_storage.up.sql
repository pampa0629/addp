BEGIN;

CREATE FUNCTION system.valid_oauth_grant_types(values_to_check text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT system.valid_distinct_text_array(values_to_check, true)
       AND values_to_check <@ ARRAY[
           'authorization_code',
           'refresh_token',
           'urn:ietf:params:oauth:grant-type:device_code'
       ]::text[];
$$;

CREATE TABLE system.oauth_clients (
    client_id text PRIMARY KEY CHECK (client_id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{1,127}$'),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    client_type text NOT NULL CHECK (client_type IN ('public', 'confidential')),
    client_secret_hash text,
    redirect_uris text[] NOT NULL CHECK (system.valid_distinct_text_array(redirect_uris, true)),
    grant_types text[] NOT NULL CHECK (system.valid_oauth_grant_types(grant_types)),
    response_types text[] NOT NULL CHECK (
        system.valid_distinct_text_array(response_types, true)
        AND response_types <@ ARRAY['code']::text[]
    ),
    allowed_scopes text[] NOT NULL CHECK (system.valid_distinct_text_array(allowed_scopes, true)),
    allowed_audiences text[] NOT NULL CHECK (system.valid_distinct_text_array(allowed_audiences, true)),
    token_endpoint_auth_method text NOT NULL CHECK (
        token_endpoint_auth_method IN ('none', 'client_secret_basic', 'private_key_jwt')
    ),
    jwks_uri text,
    jwks jsonb,
    request_uris text[] NOT NULL DEFAULT ARRAY[]::text[] CHECK (
        system.valid_distinct_text_array(request_uris, false)
    ),
    id_token_signed_response_alg text,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (client_secret_hash IS NULL OR btrim(client_secret_hash) <> ''),
    CHECK (jwks_uri IS NULL OR btrim(jwks_uri) <> ''),
    CHECK (jwks IS NULL OR jsonb_typeof(jwks) = 'object'),
    CHECK (NOT (jwks_uri IS NOT NULL AND jwks IS NOT NULL)),
    CHECK (id_token_signed_response_alg IS NULL OR btrim(id_token_signed_response_alg) <> ''),
    CHECK (
        (client_type = 'public'
            AND client_secret_hash IS NULL
            AND token_endpoint_auth_method = 'none')
        OR (client_type = 'confidential'
            AND token_endpoint_auth_method = 'client_secret_basic'
            AND client_secret_hash IS NOT NULL)
        OR (client_type = 'confidential'
            AND token_endpoint_auth_method = 'private_key_jwt'
            AND client_secret_hash IS NULL
            AND (jwks_uri IS NOT NULL OR jwks IS NOT NULL))
    )
);

CREATE TABLE system.oauth_authorization_requests (
    id uuid PRIMARY KEY,
    request_secret_hash char(64) NOT NULL UNIQUE CHECK (request_secret_hash ~ '^[0-9a-f]{64}$'),
    client_id text NOT NULL REFERENCES system.oauth_clients(client_id),
    redirect_uri text NOT NULL CHECK (btrim(redirect_uri) <> ''),
    response_types text[] NOT NULL CHECK (
        system.valid_distinct_text_array(response_types, true)
        AND response_types <@ ARRAY['code']::text[]
    ),
    response_mode text NOT NULL CHECK (response_mode IN ('query', 'form_post')),
    requested_scopes text[] NOT NULL CHECK (system.valid_distinct_text_array(requested_scopes, true)),
    requested_audiences text[] NOT NULL CHECK (system.valid_distinct_text_array(requested_audiences, true)),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    principal_id bigint REFERENCES system.principals(id),
    context_type text CHECK (context_type IN ('platform', 'tenant')),
    tenant_membership_id bigint REFERENCES system.tenant_memberships(id),
    issued_authorization_version bigint CHECK (issued_authorization_version > 0),
    granted_scopes text[],
    granted_audiences text[],
    authentication_methods text[],
    assurance_level text CHECK (assurance_level IN ('aal1', 'aal2', 'aal3')),
    authenticated_at timestamptz,
    requested_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (requested_at <= created_at),
    CHECK (expires_at > created_at),
    CHECK (completed_at IS NULL OR (completed_at >= created_at AND completed_at <= expires_at)),
    CHECK (granted_scopes IS NULL OR system.valid_distinct_text_array(granted_scopes, true)),
    CHECK (granted_audiences IS NULL OR system.valid_distinct_text_array(granted_audiences, true)),
    CHECK (authentication_methods IS NULL OR system.valid_distinct_text_array(authentication_methods, true)),
    CHECK (
        (status = 'pending'
            AND principal_id IS NULL
            AND context_type IS NULL
            AND tenant_membership_id IS NULL
            AND issued_authorization_version IS NULL
            AND granted_scopes IS NULL
            AND granted_audiences IS NULL
            AND authentication_methods IS NULL
            AND assurance_level IS NULL
            AND authenticated_at IS NULL
            AND completed_at IS NULL)
        OR (status = 'approved'
            AND principal_id IS NOT NULL
            AND context_type IS NOT NULL
            AND issued_authorization_version IS NOT NULL
            AND granted_scopes IS NOT NULL
            AND granted_audiences IS NOT NULL
            AND authentication_methods IS NOT NULL
            AND assurance_level IS NOT NULL
            AND authenticated_at IS NOT NULL
            AND completed_at IS NOT NULL
            AND ((context_type = 'platform' AND tenant_membership_id IS NULL)
                OR (context_type = 'tenant' AND tenant_membership_id IS NOT NULL)))
        OR (status IN ('rejected', 'cancelled')
            AND principal_id IS NULL
            AND context_type IS NULL
            AND tenant_membership_id IS NULL
            AND issued_authorization_version IS NULL
            AND granted_scopes IS NULL
            AND granted_audiences IS NULL
            AND authentication_methods IS NULL
            AND assurance_level IS NULL
            AND authenticated_at IS NULL
            AND completed_at IS NOT NULL)
    )
);

CREATE INDEX idx_oauth_authorization_requests_client
    ON system.oauth_authorization_requests (client_id);
CREATE INDEX idx_oauth_authorization_requests_principal
    ON system.oauth_authorization_requests (principal_id)
    WHERE principal_id IS NOT NULL;
CREATE INDEX idx_oauth_authorization_requests_membership
    ON system.oauth_authorization_requests (tenant_membership_id)
    WHERE tenant_membership_id IS NOT NULL;
CREATE INDEX idx_oauth_authorization_requests_pending_expiry
    ON system.oauth_authorization_requests (expires_at)
    WHERE status = 'pending';

CREATE TABLE system.oauth_pkce_sessions (
    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    authorization_request_id uuid NOT NULL UNIQUE REFERENCES system.oauth_authorization_requests(id),
    authorization_code_hash char(64) UNIQUE CHECK (
        authorization_code_hash IS NULL OR authorization_code_hash ~ '^[0-9a-f]{64}$'
    ),
    code_challenge text NOT NULL CHECK (btrim(code_challenge) <> ''),
    code_challenge_method text NOT NULL CHECK (code_challenge_method = 'S256'),
    verified_at timestamptz,
    consumed_at timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (verified_at IS NULL OR authorization_code_hash IS NOT NULL),
    CHECK (consumed_at IS NULL OR verified_at IS NOT NULL),
    CHECK (verified_at IS NULL OR (verified_at >= created_at AND verified_at <= expires_at)),
    CHECK (consumed_at IS NULL OR (consumed_at >= verified_at AND consumed_at <= expires_at))
);

CREATE TABLE system.oauth_oidc_sessions (
    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    authorization_request_id uuid NOT NULL UNIQUE REFERENCES system.oauth_authorization_requests(id),
    authorization_code_hash char(64) UNIQUE CHECK (
        authorization_code_hash IS NULL OR authorization_code_hash ~ '^[0-9a-f]{64}$'
    ),
    subject text,
    nonce text,
    requested_at timestamptz NOT NULL,
    authenticated_at timestamptz,
    acr text,
    amr text[],
    extra_claims_schema_version smallint,
    extra_claims jsonb,
    consumed_at timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (subject IS NULL OR btrim(subject) <> ''),
    CHECK (nonce IS NULL OR btrim(nonce) <> ''),
    CHECK (acr IS NULL OR btrim(acr) <> ''),
    CHECK (amr IS NULL OR system.valid_distinct_text_array(amr, true)),
    CHECK (requested_at <= created_at),
    CHECK (expires_at > created_at),
    CHECK (
        (authorization_code_hash IS NULL
            AND subject IS NULL
            AND authenticated_at IS NULL
            AND acr IS NULL
            AND amr IS NULL
            AND extra_claims_schema_version IS NULL
            AND extra_claims IS NULL)
        OR (authorization_code_hash IS NOT NULL
            AND subject IS NOT NULL
            AND authenticated_at IS NOT NULL
            AND acr IS NOT NULL
            AND amr IS NOT NULL
            AND extra_claims_schema_version IS NOT NULL
            AND extra_claims_schema_version > 0
            AND extra_claims IS NOT NULL
            AND jsonb_typeof(extra_claims) = 'object')
    ),
    CHECK (authenticated_at IS NULL OR authenticated_at <= expires_at),
    CHECK (consumed_at IS NULL OR authorization_code_hash IS NOT NULL),
    CHECK (consumed_at IS NULL OR (consumed_at >= created_at AND consumed_at <= expires_at))
);

CREATE TABLE system.oauth_authorization_codes (
    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    code_hash char(64) NOT NULL UNIQUE CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    authorization_request_id uuid NOT NULL UNIQUE REFERENCES system.oauth_authorization_requests(id),
    expires_at timestamptz NOT NULL,
    invalidated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (invalidated_at IS NULL OR (invalidated_at >= created_at AND invalidated_at <= expires_at))
);

CREATE INDEX idx_oauth_authorization_codes_active_expiry
    ON system.oauth_authorization_codes (expires_at)
    WHERE invalidated_at IS NULL;

CREATE TABLE system.oauth_device_authorizations (
    id uuid PRIMARY KEY,
    device_code_hash char(64) NOT NULL UNIQUE CHECK (device_code_hash ~ '^[0-9a-f]{64}$'),
    user_code_hash char(64) NOT NULL UNIQUE CHECK (user_code_hash ~ '^[0-9a-f]{64}$'),
    client_id text NOT NULL REFERENCES system.oauth_clients(client_id),
    requested_scopes text[] NOT NULL CHECK (system.valid_distinct_text_array(requested_scopes, true)),
    requested_audiences text[] NOT NULL CHECK (system.valid_distinct_text_array(requested_audiences, true)),
    granted_scopes text[],
    granted_audiences text[],
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'invalidated')),
    principal_id bigint REFERENCES system.principals(id),
    context_type text CHECK (context_type IN ('platform', 'tenant')),
    tenant_membership_id bigint REFERENCES system.tenant_memberships(id),
    issued_authorization_version bigint CHECK (issued_authorization_version > 0),
    authentication_methods text[],
    assurance_level text CHECK (assurance_level IN ('aal1', 'aal2', 'aal3')),
    authenticated_at timestamptz,
    poll_interval_seconds integer NOT NULL DEFAULT 5 CHECK (poll_interval_seconds >= 5),
    next_poll_at timestamptz NOT NULL,
    last_polled_at timestamptz,
    requested_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    decided_at timestamptz,
    invalidated_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (granted_scopes IS NULL OR system.valid_distinct_text_array(granted_scopes, true)),
    CHECK (granted_audiences IS NULL OR system.valid_distinct_text_array(granted_audiences, true)),
    CHECK (authentication_methods IS NULL OR system.valid_distinct_text_array(authentication_methods, true)),
    CHECK (requested_at <= created_at),
    CHECK (expires_at > created_at),
    CHECK (next_poll_at >= requested_at),
    CHECK (last_polled_at IS NULL OR (last_polled_at >= requested_at AND last_polled_at <= expires_at)),
    CHECK (decided_at IS NULL OR (decided_at >= created_at AND decided_at <= expires_at)),
    CHECK (invalidated_at IS NULL OR (invalidated_at >= created_at AND invalidated_at <= expires_at)),
    CHECK (
        (status = 'pending'
            AND granted_scopes IS NULL
            AND granted_audiences IS NULL
            AND principal_id IS NULL
            AND context_type IS NULL
            AND tenant_membership_id IS NULL
            AND issued_authorization_version IS NULL
            AND authentication_methods IS NULL
            AND assurance_level IS NULL
            AND authenticated_at IS NULL
            AND decided_at IS NULL
            AND invalidated_at IS NULL)
        OR (status = 'approved'
            AND granted_scopes IS NOT NULL
            AND granted_audiences IS NOT NULL
            AND principal_id IS NOT NULL
            AND context_type IS NOT NULL
            AND issued_authorization_version IS NOT NULL
            AND authentication_methods IS NOT NULL
            AND assurance_level IS NOT NULL
            AND authenticated_at IS NOT NULL
            AND decided_at IS NOT NULL
            AND invalidated_at IS NULL
            AND ((context_type = 'platform' AND tenant_membership_id IS NULL)
                OR (context_type = 'tenant' AND tenant_membership_id IS NOT NULL)))
        OR (status = 'rejected'
            AND granted_scopes IS NULL
            AND granted_audiences IS NULL
            AND principal_id IS NULL
            AND context_type IS NULL
            AND tenant_membership_id IS NULL
            AND issued_authorization_version IS NULL
            AND authentication_methods IS NULL
            AND assurance_level IS NULL
            AND authenticated_at IS NULL
            AND decided_at IS NOT NULL
            AND invalidated_at IS NULL)
        OR (status = 'invalidated'
            AND granted_scopes IS NOT NULL
            AND granted_audiences IS NOT NULL
            AND principal_id IS NOT NULL
            AND context_type IS NOT NULL
            AND issued_authorization_version IS NOT NULL
            AND authentication_methods IS NOT NULL
            AND assurance_level IS NOT NULL
            AND authenticated_at IS NOT NULL
            AND decided_at IS NOT NULL
            AND invalidated_at IS NOT NULL
            AND ((context_type = 'platform' AND tenant_membership_id IS NULL)
                OR (context_type = 'tenant' AND tenant_membership_id IS NOT NULL)))
    )
);

CREATE INDEX idx_oauth_device_authorizations_client
    ON system.oauth_device_authorizations (client_id);
CREATE INDEX idx_oauth_device_authorizations_principal
    ON system.oauth_device_authorizations (principal_id)
    WHERE principal_id IS NOT NULL;
CREATE INDEX idx_oauth_device_authorizations_membership
    ON system.oauth_device_authorizations (tenant_membership_id)
    WHERE tenant_membership_id IS NOT NULL;
CREATE INDEX idx_oauth_device_authorizations_pending_expiry
    ON system.oauth_device_authorizations (expires_at, next_poll_at)
    WHERE status = 'pending';

CREATE FUNCTION system.oauth_identity_context_is_valid(
    target_principal_id bigint,
    target_context_type text,
    target_membership_id bigint,
    target_authorization_version bigint,
    target_assurance_level text
)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM system.principals principal
        WHERE principal.id = target_principal_id
          AND principal.principal_type = 'user'
          AND principal.status = 'active'
          AND principal.authorization_version = target_authorization_version
          AND (
              (target_context_type = 'platform'
                  AND target_membership_id IS NULL
                  AND target_assurance_level IN ('aal2', 'aal3')
                  AND EXISTS (
                      SELECT 1
                      FROM system.role_assignments assignment
                      WHERE assignment.principal_id = principal.id
                        AND assignment.scope_type = 'platform'
                        AND assignment.status = 'active'
                        AND assignment.valid_from <= now()
                        AND (assignment.valid_until IS NULL OR assignment.valid_until > now())
                  ))
              OR (target_context_type = 'tenant'
                  AND target_membership_id IS NOT NULL
                  AND EXISTS (
                      SELECT 1
                      FROM system.tenant_memberships membership
                      JOIN system.tenants tenant ON tenant.id = membership.tenant_id
                      WHERE membership.id = target_membership_id
                        AND membership.principal_id = principal.id
                        AND membership.status = 'active'
                        AND tenant.status = 'active'
                        AND (membership.expires_at IS NULL OR membership.expires_at > now())
                  ))
          )
    );
$$;

CREATE FUNCTION system.validate_oauth_authorization_request()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_client system.oauth_clients%ROWTYPE;
BEGIN
    SELECT * INTO target_client
    FROM system.oauth_clients
    WHERE client_id = NEW.client_id
    FOR KEY SHARE;

    IF TG_OP = 'INSERT' OR NEW.status = 'approved' THEN
        IF target_client.status <> 'active'
           OR NOT ('authorization_code' = ANY(target_client.grant_types))
           OR NOT (NEW.requested_scopes <@ target_client.allowed_scopes)
           OR NOT (NEW.requested_audiences <@ target_client.allowed_audiences) THEN
            RAISE EXCEPTION 'authorization request exceeds the active client registration'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF TG_OP = 'INSERT' AND NEW.status <> 'pending' THEN
        RAISE EXCEPTION 'authorization request must be created pending'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status <> 'pending'
           OR NEW.id <> OLD.id
           OR NEW.request_secret_hash <> OLD.request_secret_hash
           OR NEW.client_id <> OLD.client_id
           OR NEW.redirect_uri <> OLD.redirect_uri
           OR NEW.response_types <> OLD.response_types
           OR NEW.response_mode <> OLD.response_mode
           OR NEW.requested_scopes <> OLD.requested_scopes
           OR NEW.requested_audiences <> OLD.requested_audiences
           OR NEW.requested_at <> OLD.requested_at
           OR NEW.expires_at <> OLD.expires_at
           OR NEW.created_at <> OLD.created_at THEN
            RAISE EXCEPTION 'authorization request protocol facts and terminal state are immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.status <> 'pending' AND NEW.completed_at > now() THEN
        RAISE EXCEPTION 'authorization request completion cannot be in the future'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.status = 'approved' THEN
        IF NEW.authenticated_at > NEW.completed_at
           OR NOT (NEW.granted_scopes <@ NEW.requested_scopes)
           OR NOT (NEW.granted_audiences <@ NEW.requested_audiences)
           OR NOT system.oauth_identity_context_is_valid(
               NEW.principal_id,
               NEW.context_type,
               NEW.tenant_membership_id,
               NEW.issued_authorization_version,
               NEW.assurance_level
           ) THEN
            RAISE EXCEPTION 'approved authorization request has invalid identity, context, or grant facts'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_oauth_pkce_session()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_request system.oauth_authorization_requests%ROWTYPE;
BEGIN
    SELECT * INTO target_request
    FROM system.oauth_authorization_requests
    WHERE id = NEW.authorization_request_id
    FOR KEY SHARE;

    IF NEW.expires_at > target_request.expires_at
       OR (TG_OP = 'INSERT' AND (
           target_request.status <> 'pending'
           OR NEW.authorization_code_hash IS NOT NULL
           OR NEW.verified_at IS NOT NULL
           OR NEW.consumed_at IS NOT NULL
       ))
       OR (NEW.authorization_code_hash IS NOT NULL AND target_request.status <> 'approved') THEN
        RAISE EXCEPTION 'PKCE session expiry exceeds its authorization request'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id <> OLD.id
           OR NEW.authorization_request_id <> OLD.authorization_request_id
           OR (OLD.authorization_code_hash IS NOT NULL
               AND NEW.authorization_code_hash IS DISTINCT FROM OLD.authorization_code_hash)
           OR NEW.code_challenge <> OLD.code_challenge
           OR NEW.code_challenge_method <> OLD.code_challenge_method
           OR NEW.expires_at <> OLD.expires_at
           OR NEW.created_at <> OLD.created_at
           OR (OLD.verified_at IS NOT NULL AND NEW.verified_at IS DISTINCT FROM OLD.verified_at)
           OR (OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at) THEN
            RAISE EXCEPTION 'PKCE session facts and terminal timestamps are immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_oauth_oidc_session()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_request system.oauth_authorization_requests%ROWTYPE;
BEGIN
    SELECT * INTO target_request
    FROM system.oauth_authorization_requests
    WHERE id = NEW.authorization_request_id
    FOR KEY SHARE;

    IF NOT ('openid' = ANY(target_request.requested_scopes))
       OR NEW.requested_at <> target_request.requested_at
       OR NEW.expires_at > target_request.expires_at
       OR (TG_OP = 'INSERT' AND (
           target_request.status <> 'pending'
           OR NEW.authorization_code_hash IS NOT NULL
           OR NEW.subject IS NOT NULL
           OR NEW.consumed_at IS NOT NULL
       ))
       OR ((NEW.authorization_code_hash IS NOT NULL OR NEW.subject IS NOT NULL)
           AND target_request.status <> 'approved')
       OR (NEW.authenticated_at IS NOT NULL AND NEW.authenticated_at > now()) THEN
        RAISE EXCEPTION 'OIDC session requires an openid authorization request with bounded expiry'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.id <> OLD.id
           OR NEW.authorization_request_id <> OLD.authorization_request_id
           OR (OLD.authorization_code_hash IS NOT NULL
               AND NEW.authorization_code_hash IS DISTINCT FROM OLD.authorization_code_hash)
           OR NEW.nonce IS DISTINCT FROM OLD.nonce
           OR NEW.requested_at <> OLD.requested_at
           OR NEW.expires_at <> OLD.expires_at
           OR NEW.created_at <> OLD.created_at
           OR (OLD.subject IS NOT NULL AND NEW.subject IS DISTINCT FROM OLD.subject)
           OR (OLD.authenticated_at IS NOT NULL AND NEW.authenticated_at IS DISTINCT FROM OLD.authenticated_at)
           OR (OLD.acr IS NOT NULL AND NEW.acr IS DISTINCT FROM OLD.acr)
           OR (OLD.amr IS NOT NULL AND NEW.amr IS DISTINCT FROM OLD.amr)
           OR (OLD.extra_claims_schema_version IS NOT NULL
               AND NEW.extra_claims_schema_version IS DISTINCT FROM OLD.extra_claims_schema_version)
           OR (OLD.extra_claims IS NOT NULL AND NEW.extra_claims IS DISTINCT FROM OLD.extra_claims)
           OR (OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at) THEN
            RAISE EXCEPTION 'OIDC session facts and terminal timestamps are immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_oauth_authorization_code()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_request system.oauth_authorization_requests%ROWTYPE;
BEGIN
    SELECT * INTO target_request
    FROM system.oauth_authorization_requests
    WHERE id = NEW.authorization_request_id
    FOR KEY SHARE;

    IF target_request.status <> 'approved' OR NEW.expires_at > target_request.expires_at THEN
        RAISE EXCEPTION 'authorization code requires an approved request with bounded expiry'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' AND NEW.invalidated_at IS NOT NULL THEN
        RAISE EXCEPTION 'authorization code must be created active'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND (
        NEW.id <> OLD.id
        OR NEW.code_hash <> OLD.code_hash
        OR NEW.authorization_request_id <> OLD.authorization_request_id
        OR NEW.expires_at <> OLD.expires_at
        OR NEW.created_at <> OLD.created_at
        OR OLD.invalidated_at IS NOT NULL
        OR NEW.invalidated_at IS NULL
    ) THEN
        RAISE EXCEPTION 'authorization code facts are immutable and invalidation is one-way'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_oauth_device_authorization()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_client system.oauth_clients%ROWTYPE;
BEGIN
    SELECT * INTO target_client
    FROM system.oauth_clients
    WHERE client_id = NEW.client_id
    FOR KEY SHARE;

    IF TG_OP = 'INSERT' OR NEW.status = 'approved' THEN
        IF target_client.status <> 'active'
           OR NOT ('urn:ietf:params:oauth:grant-type:device_code' = ANY(target_client.grant_types))
           OR NOT (NEW.requested_scopes <@ target_client.allowed_scopes)
           OR NOT (NEW.requested_audiences <@ target_client.allowed_audiences) THEN
            RAISE EXCEPTION 'device authorization exceeds the active client registration'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF TG_OP = 'INSERT' AND NEW.status <> 'pending' THEN
        RAISE EXCEPTION 'device authorization must be created pending'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status IN ('rejected', 'invalidated')
           OR (OLD.status = 'approved' AND NEW.status NOT IN ('approved', 'invalidated'))
           OR (OLD.status = 'pending' AND NEW.status NOT IN ('pending', 'approved', 'rejected'))
           OR NEW.id <> OLD.id
           OR NEW.device_code_hash <> OLD.device_code_hash
           OR NEW.user_code_hash <> OLD.user_code_hash
           OR NEW.client_id <> OLD.client_id
           OR NEW.requested_scopes <> OLD.requested_scopes
           OR NEW.requested_audiences <> OLD.requested_audiences
           OR NEW.requested_at <> OLD.requested_at
           OR NEW.expires_at <> OLD.expires_at
           OR NEW.created_at <> OLD.created_at
           OR NEW.poll_interval_seconds < OLD.poll_interval_seconds
           OR (OLD.last_polled_at IS NOT NULL AND NEW.last_polled_at < OLD.last_polled_at)
           OR NEW.next_poll_at < OLD.next_poll_at
           OR (OLD.decided_at IS NOT NULL AND NEW.decided_at IS DISTINCT FROM OLD.decided_at)
           OR (OLD.invalidated_at IS NOT NULL AND NEW.invalidated_at IS DISTINCT FROM OLD.invalidated_at) THEN
            RAISE EXCEPTION 'device authorization protocol and terminal facts are immutable'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    IF NEW.status <> 'pending' AND NEW.decided_at > now() THEN
        RAISE EXCEPTION 'device authorization decision cannot be in the future'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.status IN ('approved', 'invalidated') THEN
        IF NEW.authenticated_at > NEW.decided_at
           OR NOT (NEW.granted_scopes <@ NEW.requested_scopes)
           OR NOT (NEW.granted_audiences <@ NEW.requested_audiences)
           OR NOT system.oauth_identity_context_is_valid(
               NEW.principal_id,
               NEW.context_type,
               NEW.tenant_membership_id,
               NEW.issued_authorization_version,
               NEW.assurance_level
           ) THEN
            RAISE EXCEPTION 'approved device authorization has invalid identity, context, or grant facts'
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.validate_oauth_client_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.client_id <> OLD.client_id OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'OAuth client identity is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION system.prevent_oauth_client_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% history cannot be physically deleted', TG_TABLE_NAME
        USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER trg_oauth_clients_validate_update
BEFORE UPDATE ON system.oauth_clients
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_client_update();
CREATE TRIGGER trg_oauth_clients_updated_at
BEFORE UPDATE ON system.oauth_clients
FOR EACH ROW EXECUTE FUNCTION system.set_updated_at();
CREATE TRIGGER trg_oauth_clients_prevent_delete
BEFORE DELETE ON system.oauth_clients
FOR EACH ROW EXECUTE FUNCTION system.prevent_oauth_client_delete();

CREATE TRIGGER trg_oauth_authorization_requests_validate
BEFORE INSERT OR UPDATE ON system.oauth_authorization_requests
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_authorization_request();
CREATE TRIGGER trg_oauth_pkce_sessions_validate
BEFORE INSERT OR UPDATE ON system.oauth_pkce_sessions
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_pkce_session();

CREATE TRIGGER trg_oauth_oidc_sessions_validate
BEFORE INSERT OR UPDATE ON system.oauth_oidc_sessions
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_oidc_session();

CREATE TRIGGER trg_oauth_authorization_codes_validate
BEFORE INSERT OR UPDATE ON system.oauth_authorization_codes
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_authorization_code();

CREATE TRIGGER trg_oauth_device_authorizations_validate
BEFORE INSERT OR UPDATE ON system.oauth_device_authorizations
FOR EACH ROW EXECUTE FUNCTION system.validate_oauth_device_authorization();

COMMIT;
