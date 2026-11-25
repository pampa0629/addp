CREATE TRIGGER IF NOT EXISTS trg_mfa_enrollments_before_delete
BEFORE DELETE ON mfa_enrollments FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='MFA enrollment cannot be deleted'
