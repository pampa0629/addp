CREATE TRIGGER IF NOT EXISTS trg_access_tokens_before_delete
BEFORE DELETE ON access_tokens FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'access token history cannot be physically deleted'
