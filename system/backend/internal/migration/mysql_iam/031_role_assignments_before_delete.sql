CREATE TRIGGER IF NOT EXISTS trg_role_assignments_before_delete
BEFORE DELETE ON role_assignments FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'role assignment history cannot be physically deleted'
