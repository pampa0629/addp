CREATE TRIGGER IF NOT EXISTS trg_mfa_credentials_before_update
BEFORE UPDATE ON mfa_credentials FOR EACH ROW
BEGIN
    IF NEW.user_id <> OLD.user_id OR NEW.method <> OLD.method OR NEW.created_at <> OLD.created_at THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'MFA credential identity is immutable';
    END IF;
    IF NEW.status = OLD.status
       AND NEW.secret_ciphertext = OLD.secret_ciphertext
       AND NEW.secret_nonce = OLD.secret_nonce
       AND NEW.key_version = OLD.key_version
       AND (NEW.last_accepted_counter <=> OLD.last_accepted_counter) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'MFA credential update must change a security fact';
    END IF;
    IF OLD.status = 'disabled' AND NEW.status <> 'disabled' THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'disabled MFA credential cannot be reactivated';
    END IF;
    IF NOT (NEW.last_accepted_counter <=> OLD.last_accepted_counter)
       AND OLD.last_accepted_counter IS NOT NULL
       AND (NEW.last_accepted_counter IS NULL OR NEW.last_accepted_counter <= OLD.last_accepted_counter) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'MFA accepted counter must increase';
    END IF;
    SET NEW.updated_at = CURRENT_TIMESTAMP(6);
END
