CREATE TRIGGER IF NOT EXISTS execution_authorization_validate_update
BEFORE UPDATE ON execution_authorizations FOR EACH ROW
BEGIN
    IF NEW.id<>OLD.id OR NEW.actor_principal_id<>OLD.actor_principal_id OR NEW.tenant_id<>OLD.tenant_id
       OR NEW.tenant_membership_id<>OLD.tenant_membership_id OR NEW.issued_authorization_version<>OLD.issued_authorization_version
       OR NEW.source_type<>OLD.source_type OR NOT (NEW.source_definition_id <=> OLD.source_definition_id)
       OR NOT (NEW.source_definition_version <=> OLD.source_definition_version)
       OR NOT (NEW.source_execution_attempt <=> OLD.source_execution_attempt)
       OR NOT (NEW.source_execution_lease_token <=> OLD.source_execution_lease_token)
       OR NEW.execution_id<>OLD.execution_id OR NEW.audience<>OLD.audience
       OR NEW.created_at<>OLD.created_at OR NEW.expires_at<>OLD.expires_at THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization identity and boundary are immutable';
    END IF;
    IF OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL THEN
        IF NEW.sealed_at<>NEW.created_at OR NOT EXISTS (
            SELECT 1 FROM execution_authorization_engine_accesses a WHERE a.authorization_id=NEW.id FOR SHARE
        ) THEN
            SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization requires immutable engine accesses before sealing';
        END IF;
    ELSEIF NOT (NEW.sealed_at <=> OLD.sealed_at) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization seal is immutable';
    END IF;
    IF OLD.revoked_at IS NOT NULL AND (NOT (NEW.revoked_at <=> OLD.revoked_at) OR NOT (NEW.revoked_reason <=> OLD.revoked_reason)) THEN
        SIGNAL SQLSTATE '45000' SET MYSQL_ERRNO=30001, MESSAGE_TEXT='execution authorization revocation is immutable';
    END IF;
END
