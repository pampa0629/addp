CREATE TRIGGER IF NOT EXISTS trg_refresh_token_families_before_update
BEFORE UPDATE ON refresh_token_families FOR EACH ROW
BEGIN
    IF NEW.id <> OLD.id OR NOT (NEW.protocol_request_id <=> OLD.protocol_request_id)
       OR NEW.principal_id <> OLD.principal_id OR NEW.context_type <> OLD.context_type
       OR NOT (NEW.tenant_membership_id <=> OLD.tenant_membership_id)
       OR NEW.client_id <> OLD.client_id OR NEW.auth_type <> OLD.auth_type
       OR NEW.audiences <> OLD.audiences OR NEW.scopes <> OLD.scopes
       OR NEW.authentication_methods <> OLD.authentication_methods OR NEW.assurance_level <> OLD.assurance_level
       OR NEW.authenticated_at <> OLD.authenticated_at OR NOT (NEW.step_up_expires_at <=> OLD.step_up_expires_at)
       OR NOT (NEW.oidc_subject <=> OLD.oidc_subject) OR NOT (NEW.oidc_acr <=> OLD.oidc_acr)
       OR NOT (NEW.oidc_amr <=> OLD.oidc_amr) OR NOT (NEW.oidc_claims_schema_version <=> OLD.oidc_claims_schema_version)
       OR NOT (NEW.oidc_claims <=> OLD.oidc_claims) OR NEW.expires_at <> OLD.expires_at
       OR NEW.created_at <> OLD.created_at OR OLD.revoked_at IS NOT NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'token family identity and revoked facts are immutable';
    END IF;
    IF NEW.revoked_at IS NOT NULL THEN
        IF NEW.issued_authorization_version <> OLD.issued_authorization_version THEN
            SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'token family revocation cannot change authorization version';
        END IF;
    ELSEIF NEW.issued_authorization_version <= OLD.issued_authorization_version OR NOT EXISTS (
        SELECT 1 FROM principals WHERE id = NEW.principal_id AND status = 'active'
           AND authorization_version = NEW.issued_authorization_version
        FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active token family version may only advance to the current principal version';
    END IF;
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
