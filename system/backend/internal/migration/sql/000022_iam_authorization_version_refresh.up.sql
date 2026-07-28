BEGIN;

CREATE OR REPLACE FUNCTION system.validate_token_family_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_principal system.principals%ROWTYPE;
BEGIN
    IF NEW.protocol_request_id IS DISTINCT FROM OLD.protocol_request_id
       OR NEW.principal_id <> OLD.principal_id
       OR NEW.context_type <> OLD.context_type
       OR NEW.tenant_membership_id IS DISTINCT FROM OLD.tenant_membership_id
       OR NEW.client_id <> OLD.client_id
       OR NEW.auth_type <> OLD.auth_type
       OR NEW.audiences <> OLD.audiences
       OR NEW.scopes <> OLD.scopes
       OR NEW.authentication_methods <> OLD.authentication_methods
       OR NEW.assurance_level <> OLD.assurance_level
       OR NEW.authenticated_at <> OLD.authenticated_at
       OR NEW.step_up_expires_at IS DISTINCT FROM OLD.step_up_expires_at
       OR NEW.oidc_subject IS DISTINCT FROM OLD.oidc_subject
       OR NEW.oidc_acr IS DISTINCT FROM OLD.oidc_acr
       OR NEW.oidc_amr IS DISTINCT FROM OLD.oidc_amr
       OR NEW.oidc_claims_schema_version IS DISTINCT FROM OLD.oidc_claims_schema_version
       OR NEW.oidc_claims IS DISTINCT FROM OLD.oidc_claims
       OR NEW.expires_at <> OLD.expires_at
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'token family identity, context, authentication, and final expiry are immutable'
            USING ERRCODE = '23514';
    END IF;

    IF OLD.revoked_at IS NOT NULL THEN
        RAISE EXCEPTION 'revoked token family facts are immutable'
            USING ERRCODE = '23514';
    END IF;

    IF NEW.revoked_at IS NOT NULL THEN
        IF NEW.revoked_reason IS NULL
           OR NEW.issued_authorization_version <> OLD.issued_authorization_version THEN
            RAISE EXCEPTION 'token family revocation cannot change authorization version'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;

    SELECT * INTO current_principal
    FROM system.principals
    WHERE id = NEW.principal_id
    FOR KEY SHARE;

    IF NEW.revoked_reason IS NOT NULL
       OR current_principal.status <> 'active'
       OR NEW.issued_authorization_version <= OLD.issued_authorization_version
       OR NEW.issued_authorization_version <> current_principal.authorization_version THEN
        RAISE EXCEPTION 'active token family authorization version may only advance to the current principal version'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
