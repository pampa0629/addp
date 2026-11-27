CREATE TRIGGER IF NOT EXISTS notebook_validate_execution_source
BEFORE INSERT ON execution_authorizations FOR EACH ROW
BEGIN
    IF NEW.source_notebook_session_authorization_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM notebook_session_authorizations s JOIN refresh_token_families f ON f.id=s.token_family_id
        WHERE s.id=NEW.source_notebook_session_authorization_id AND s.actor_principal_id=NEW.actor_principal_id
          AND s.tenant_id=NEW.tenant_id AND s.tenant_membership_id=NEW.tenant_membership_id
          AND s.issued_authorization_version=NEW.issued_authorization_version AND s.revoked_at IS NULL
          AND s.expires_at>=NEW.expires_at AND f.revoked_at IS NULL AND f.expires_at>NEW.created_at FOR SHARE) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Notebook execution requires the matching active session';
    END IF;
END
