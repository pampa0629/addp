CREATE TRIGGER IF NOT EXISTS trg_permissions_before_delete
BEFORE DELETE ON permissions FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='permission history cannot be deleted'
