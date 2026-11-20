CREATE TRIGGER IF NOT EXISTS trg_refresh_token_families_before_delete
BEFORE DELETE ON refresh_token_families FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'token family history cannot be physically deleted'
