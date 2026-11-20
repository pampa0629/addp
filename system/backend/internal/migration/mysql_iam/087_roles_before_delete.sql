CREATE TRIGGER IF NOT EXISTS trg_roles_before_delete
BEFORE DELETE ON roles FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='role history cannot be deleted'
