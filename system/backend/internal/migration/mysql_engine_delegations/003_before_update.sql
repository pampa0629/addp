CREATE TRIGGER IF NOT EXISTS engine_delegation_revoke
BEFORE UPDATE ON engine_access_delegations FOR EACH ROW
BEGIN
    IF OLD.expires_at<=SYSDATE(6) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine_access_delegations_expired_history';
    END IF;
    IF OLD.status<>'active' OR NEW.status<>'revoked' OR NEW.version<>OLD.version+1
        OR NEW.id<>OLD.id OR NEW.tenant_id<>OLD.tenant_id OR NEW.engine_id<>OLD.engine_id
        OR NEW.tenant_membership_id<>OLD.tenant_membership_id OR NEW.granted_by_principal_id<>OLD.granted_by_principal_id
        OR NEW.granted_at<>OLD.granted_at OR NEW.expires_at<>OLD.expires_at OR BINARY NEW.grant_reason<>BINARY OLD.grant_reason THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine access delegation is immutable except versioned revocation';
    END IF;
END
