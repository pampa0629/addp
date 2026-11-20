CREATE TRIGGER IF NOT EXISTS trg_mfa_credentials_before_delete
BEFORE DELETE ON mfa_credentials FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'IAM MFA credentials cannot be physically deleted'
