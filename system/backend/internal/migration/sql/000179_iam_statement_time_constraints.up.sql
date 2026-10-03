BEGIN;

-- Validate current events and authorization against database statement time,
-- preserving existing function identities, constraints and historical facts.

CREATE OR REPLACE FUNCTION system.has_stable_tenant_administrator(target_tenant_id bigint)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM system.role_assignments assignment
        JOIN system.roles role ON role.id = assignment.role_id
        JOIN system.principals principal ON principal.id = assignment.principal_id
        JOIN system.tenant_memberships membership
          ON membership.tenant_id = assignment.tenant_id
         AND membership.principal_id = assignment.principal_id
        WHERE assignment.tenant_id = target_tenant_id
          AND assignment.scope_type = 'tenant'
          AND assignment.status = 'active'
          AND assignment.valid_from <= statement_timestamp()
          AND assignment.valid_until IS NULL
          AND role.tenant_id IS NULL
          AND role.role_key = 'tenant.administrator'
          AND role.status = 'active'
          AND principal.status = 'active'
          AND membership.status = 'active'
          AND membership.expires_at IS NULL
    );
$$;

CREATE OR REPLACE FUNCTION system.oauth_identity_context_is_valid(
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
                        AND assignment.valid_from <= statement_timestamp()
                        AND (assignment.valid_until IS NULL OR assignment.valid_until > statement_timestamp())
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
                        AND (membership.expires_at IS NULL OR membership.expires_at > statement_timestamp())
                  ))
          )
    );
$$;

CREATE OR REPLACE FUNCTION system.validate_oauth_authorization_request()
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
    IF NEW.status <> 'pending' AND NEW.completed_at > statement_timestamp() THEN
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

CREATE OR REPLACE FUNCTION system.validate_oauth_oidc_session()
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
       OR (NEW.authenticated_at IS NOT NULL AND NEW.authenticated_at > statement_timestamp()) THEN
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

CREATE OR REPLACE FUNCTION system.validate_oauth_device_authorization()
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
    IF NEW.status <> 'pending' AND NEW.decided_at > statement_timestamp() THEN
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

COMMIT;
