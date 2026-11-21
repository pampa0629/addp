CREATE TRIGGER IF NOT EXISTS engine_delegation_keep_history
BEFORE DELETE ON engine_access_delegations FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='engine access delegation history cannot be deleted'
