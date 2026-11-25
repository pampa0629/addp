CREATE TRIGGER IF NOT EXISTS trg_tenant_invitations_before_update
BEFORE UPDATE ON tenant_invitations FOR EACH ROW
BEGIN
    IF NEW.tenant_id<>OLD.tenant_id OR NEW.email<>OLD.email OR NEW.normalized_email<>OLD.normalized_email
        OR NEW.secret_hash<>OLD.secret_hash OR NEW.expires_at<>OLD.expires_at
        OR NEW.created_by_principal_id<>OLD.created_by_principal_id OR NEW.created_at<>OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='tenant invitation identity, recipient, secret, expiry and creator are immutable';
    END IF;
    IF OLD.status<>'pending' OR NEW.status NOT IN ('accepted','revoked','expired') THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid tenant invitation status transition';
    END IF;
    SET NEW.updated_at=CURRENT_TIMESTAMP(6);
END
