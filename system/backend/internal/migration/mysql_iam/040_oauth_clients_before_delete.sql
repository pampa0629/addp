CREATE TRIGGER IF NOT EXISTS trg_oauth_clients_before_delete
BEFORE DELETE ON oauth_clients FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'OAuth clients cannot be physically deleted'
