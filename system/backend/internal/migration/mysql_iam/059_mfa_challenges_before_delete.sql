CREATE TRIGGER IF NOT EXISTS trg_mfa_challenges_before_delete
BEFORE DELETE ON mfa_challenges FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'MFA challenge history cannot be physically deleted'
