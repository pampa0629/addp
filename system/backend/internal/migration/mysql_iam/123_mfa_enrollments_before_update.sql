CREATE TRIGGER IF NOT EXISTS trg_mfa_enrollments_before_update
BEFORE UPDATE ON mfa_enrollments FOR EACH ROW
BEGIN
    IF NEW.token_hash<>OLD.token_hash OR NEW.principal_id<>OLD.principal_id
       OR NEW.source_family_id<>OLD.source_family_id OR NEW.issued_authorization_version<>OLD.issued_authorization_version
       OR NEW.secret_ciphertext<>OLD.secret_ciphertext OR NEW.secret_nonce<>OLD.secret_nonce
       OR NEW.key_version<>OLD.key_version OR NEW.expires_at<>OLD.expires_at OR NEW.created_at<>OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='MFA enrollment binding is immutable';
    END IF;
    IF OLD.consumed_at IS NOT NULL OR NEW.failed_attempts NOT IN (OLD.failed_attempts,OLD.failed_attempts+1)
       OR (NEW.consumed_at IS NULL AND (NEW.failed_attempts=OLD.failed_attempts OR NEW.failed_attempts=5)) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid MFA enrollment transition';
    END IF;
END
