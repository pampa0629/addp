CREATE TRIGGER IF NOT EXISTS notebook_session_validate_update
BEFORE UPDATE ON notebook_session_authorizations FOR EACH ROW
BEGIN
    IF NEW.id<>OLD.id OR NEW.session_id<>OLD.session_id OR NEW.task_id<>OLD.task_id OR NEW.token_family_id<>OLD.token_family_id
       OR NEW.actor_principal_id<>OLD.actor_principal_id OR NEW.tenant_id<>OLD.tenant_id OR NEW.tenant_membership_id<>OLD.tenant_membership_id
       OR NEW.issued_authorization_version<>OLD.issued_authorization_version OR NEW.audience<>OLD.audience OR NEW.operations<>OLD.operations
       OR NEW.expires_at<>OLD.expires_at OR NEW.created_at<>OLD.created_at
       OR (OLD.revoked_at IS NOT NULL AND (NOT (NEW.revoked_at <=> OLD.revoked_at) OR NOT (NEW.revoked_reason <=> OLD.revoked_reason))) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='Notebook session identity, boundary and recorded revocation are immutable';
    END IF;
END
