CREATE TRIGGER IF NOT EXISTS trg_refresh_tokens_before_delete
BEFORE DELETE ON refresh_tokens FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'refresh token history cannot be physically deleted'
