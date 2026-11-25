CREATE TRIGGER IF NOT EXISTS trg_tenant_invitations_before_delete
BEFORE DELETE ON tenant_invitations FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='tenant invitation history cannot be physically deleted'
